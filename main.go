package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/itchyny/gojq"
	"github.com/spf13/cobra"
)

//go:embed openapi.json
var specBytes []byte
var version = "0.1.0-poc"

type Operation struct {
	ID      string         `json:"operationId"`
	Method  string         `json:"method"`
	Path    string         `json:"path"`
	Summary string         `json:"summary"`
	Spec    map[string]any `json:"spec,omitempty"`
}

func operations() []Operation {
	var s struct {
		Paths map[string]map[string]json.RawMessage
	}
	must(json.Unmarshal(specBytes, &s))
	out := []Operation{}
	for p, methods := range s.Paths {
		for m, raw := range methods {
			if !strings.Contains(" get post put patch delete head options ", " "+m+" ") {
				continue
			}
			var v map[string]any
			must(json.Unmarshal(raw, &v))
			if shared, ok := methods["parameters"]; ok {
				var params any
				must(json.Unmarshal(shared, &params))
				v["x-path-parameters"] = params
			}
			id, _ := v["operationId"].(string)
			summary, _ := v["summary"].(string)
			out = append(out, Operation{id, strings.ToUpper(m), p, summary, v})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func must(e error) {
	if e != nil {
		panic(e)
	}
}

type options struct {
	base, token, platform, account, auth, jq string
	json, plain                              bool
	timeout                                  time.Duration
}

var opt options

type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body) }
func exitCode(err error) int {
	var e *apiError
	if errors.As(err, &e) {
		if e.Status == 401 || e.Status == 403 {
			return 2
		}
		if e.Status == 404 {
			return 3
		}
	}
	return 1
}
func request(ctx context.Context, method, path string, q url.Values, body io.Reader, contentType string) (any, error) {
	u, e := url.Parse(opt.base)
	if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("set --base-url to an http(s) Chatwoot server URL")
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.Contains(path, "?") || strings.Contains(path, "#") {
		return nil, errors.New("API path must be absolute, without query or fragment")
	}
	escapedPath := strings.TrimRight(u.EscapedPath(), "/") + path
	decodedPath, e := url.PathUnescape(escapedPath)
	if e != nil {
		return nil, e
	}
	u.Path = decodedPath
	u.RawPath = escapedPath
	u.RawQuery = q.Encode()
	r, e := http.NewRequestWithContext(ctx, method, u.String(), body)
	if e != nil {
		return nil, e
	}
	r.Header.Set("Accept", "application/json")
	r.Header.Set("User-Agent", "chatwoot-cli/"+version)
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	token := opt.token
	if strings.HasPrefix(path, "/platform/") {
		token = opt.platform
	}
	if !strings.HasPrefix(path, "/public/") && !strings.HasPrefix(path, "/survey/") {
		if token == "" {
			return nil, errors.New("API token required (platform routes need CHATWOOT_PLATFORM_TOKEN)")
		}
		if opt.auth == "bearer" {
			r.Header.Set("Authorization", "Bearer "+token)
		} else {
			r.Header.Set("api_access_token", token)
		}
	}
	c := http.Client{Timeout: opt.timeout, CheckRedirect: func(r *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := c.Do(r)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	b, e := io.ReadAll(io.LimitReader(resp.Body, (64<<20)+1))
	if e != nil {
		return nil, e
	}
	if len(b) > 64<<20 {
		return nil, errors.New("response exceeds 64 MiB limit; narrow the request")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &apiError{resp.StatusCode, string(b)}
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return map[string]any{"status": resp.StatusCode}, nil
	}
	var out any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if e = dec.Decode(&out); e != nil {
		return map[string]any{"status": resp.StatusCode, "content_type": resp.Header.Get("Content-Type"), "body": string(b)}, nil
	}
	return out, nil
}
func emit(v any) error {
	if opt.jq != "" {
		q, e := gojq.Parse(opt.jq)
		if e != nil {
			return e
		}
		b, _ := json.Marshal(v)
		var normalized any
		_ = json.Unmarshal(b, &normalized)
		it := q.Run(normalized)
		for {
			x, ok := it.Next()
			if !ok {
				break
			}
			if e, ok := x.(error); ok {
				return e
			}
			if e := json.NewEncoder(os.Stdout).Encode(x); e != nil {
				return e
			}
		}
		return nil
	}
	if opt.plain {
		b, e := json.Marshal(v)
		if e != nil {
			return e
		}
		var n any
		_ = json.Unmarshal(b, &n)
		plain(n, "")
		return nil
	}
	enc := json.NewEncoder(os.Stdout)
	if !opt.json {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(v)
}
func plain(v any, path string) {
	switch x := v.(type) {
	case map[string]any:
		keys := []string{}
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p := k
			if path != "" {
				p = path + "." + k
			}
			plain(x[k], p)
		}
	case []any:
		for i, y := range x {
			plain(y, fmt.Sprintf("%s[%d]", path, i))
		}
	default:
		b, _ := json.Marshal(x)
		fmt.Printf("%s\t%s\n", path, b)
	}
}
func accountPath(s string) (string, error) {
	if opt.account == "" {
		return "", errors.New("--account-id or CHATWOOT_ACCOUNT_ID required")
	}
	if id, e := strconv.ParseUint(opt.account, 10, 64); e != nil || id == 0 {
		return "", errors.New("account ID must be a positive integer")
	}
	return "/api/v1/accounts/" + opt.account + s, nil
}
func callAccount(ctx context.Context, m, p string, q url.Values, v any) (any, error) {
	path, e := accountPath(p)
	if e != nil {
		return nil, e
	}
	var b io.Reader
	ct := ""
	if v != nil {
		raw, e := json.Marshal(v)
		if e != nil {
			return nil, e
		}
		b = bytes.NewReader(raw)
		ct = "application/json"
	}
	return request(ctx, m, path, q, b, ct)
}
func pairs(values []string) (url.Values, error) {
	v := url.Values{}
	for _, s := range values {
		k, x, ok := strings.Cut(s, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("expected key=value: %s", s)
		}
		v.Add(k, x)
	}
	return v, nil
}
func bodyInput(data string, forms, files []string) (io.Reader, string, error) {
	if data != "" && (len(forms)+len(files) > 0) {
		return nil, "", errors.New("--data cannot be combined with --form/--file")
	}
	if data != "" {
		var b []byte
		var e error
		if data == "-" {
			b, e = io.ReadAll(os.Stdin)
		} else if strings.HasPrefix(data, "@") {
			b, e = os.ReadFile(data[1:])
		} else {
			b = []byte(data)
		}
		if e != nil {
			return nil, "", e
		}
		if !json.Valid(b) {
			return nil, "", errors.New("--data must contain valid JSON")
		}
		return bytes.NewReader(b), "application/json", nil
	}
	if len(forms)+len(files) == 0 {
		return nil, "", nil
	}
	b := &bytes.Buffer{}
	w := multipart.NewWriter(b)
	v, e := pairs(forms)
	if e != nil {
		return nil, "", e
	}
	for k, vs := range v {
		for _, x := range vs {
			if e = w.WriteField(k, x); e != nil {
				return nil, "", e
			}
		}
	}
	for _, s := range files {
		k, p, ok := strings.Cut(s, "=")
		if !ok {
			return nil, "", errors.New("--file requires field=path")
		}
		f, e := os.Open(p)
		if e != nil {
			return nil, "", e
		}
		part, e := w.CreateFormFile(k, filepath.Base(p))
		if e == nil {
			_, e = io.Copy(part, f)
		}
		_ = f.Close()
		if e != nil {
			return nil, "", e
		}
	}
	e = w.Close()
	return b, w.FormDataContentType(), e
}
func main() {
	if e := root().Execute(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(exitCode(e))
	}
}
func root() *cobra.Command {
	r := &cobra.Command{Use: "chatwoot-cli", Short: "Real Chatwoot API CLI: tickets and complete documented operation catalog", Version: version, SilenceUsage: true, SilenceErrors: true, PersistentPreRunE: func(c *cobra.Command, a []string) error {
		if opt.json && opt.plain {
			return errors.New("--json and --plain are mutually exclusive")
		}
		if opt.jq != "" && !opt.json {
			return errors.New("--jq requires --json")
		}
		if opt.auth != "api-key" && opt.auth != "bearer" {
			return errors.New("--auth-mode must be api-key or bearer")
		}
		return nil
	}}
	f := r.PersistentFlags()
	f.StringVar(&opt.base, "base-url", os.Getenv("CHATWOOT_BASE_URL"), "Chatwoot server URL")
	token := os.Getenv("CHATWOOT_API_TOKEN")
	if token == "" {
		token = os.Getenv("CHATWOOT_AGENT_API_TOKEN")
	}
	f.StringVar(&opt.token, "token", token, "Application API token (prefer environment)")
	f.StringVar(&opt.platform, "platform-token", os.Getenv("CHATWOOT_PLATFORM_TOKEN"), "Platform API token (prefer environment)")
	f.StringVar(&opt.account, "account-id", os.Getenv("CHATWOOT_ACCOUNT_ID"), "Account ID")
	f.StringVar(&opt.auth, "auth-mode", "api-key", "api-key (legacy compatible) or bearer (4.19+)")
	f.BoolVar(&opt.json, "json", false, "Compact JSON output")
	f.BoolVar(&opt.plain, "plain", false, "Headerless TSV: JSON field path and JSON-encoded scalar")
	f.StringVar(&opt.jq, "jq", "", "In-process jq expression; requires --json")
	f.DurationVar(&opt.timeout, "timeout", 30*time.Second, "HTTP request timeout")
	r.AddCommand(apiCommands())
	addTickets(r)
	r.AddCommand(&cobra.Command{Use: "exit-codes", Run: func(c *cobra.Command, a []string) {
		fmt.Println("0 success\n1 request, validation, transport or server error\n2 authentication or permission denied (401/403)\n3 not found (404)")
	}})
	return r
}
func apiCommands() *cobra.Command {
	api := &cobra.Command{Use: "api", Short: "Discover and call every documented operation, or a raw endpoint"}
	api.AddCommand(&cobra.Command{Use: "operations [filter]", Args: cobra.MaximumNArgs(1), RunE: func(c *cobra.Command, a []string) error {
		list := []Operation{}
		for _, o := range operations() {
			if len(a) == 0 || strings.Contains(strings.ToLower(o.ID+o.Path+o.Summary), strings.ToLower(a[0])) {
				o.Spec = nil
				list = append(list, o)
			}
		}
		return emit(list)
	}})
	api.AddCommand(&cobra.Command{Use: "describe OPERATION_ID", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, a []string) error {
		for _, o := range operations() {
			if o.ID == a[0] {
				var spec any
				_ = json.Unmarshal(specBytes, &spec)
				return emit(map[string]any{"operation": o, "components": spec.(map[string]any)["components"]})
			}
		}
		return errors.New("unknown operation ID")
	}})
	for _, raw := range []bool{false, true} {
		raw := raw
		var params, queries, forms, files []string
		var data string
		cmd := &cobra.Command{Use: "call OPERATION_ID", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, a []string) error {
			var method, path string
			if raw {
				method = strings.ToUpper(a[0])
				path = a[1]
				if !strings.Contains(" GET POST PATCH PUT DELETE HEAD OPTIONS ", " "+method+" ") {
					return errors.New("unsupported HTTP method")
				}
			} else {
				for _, o := range operations() {
					if o.ID == a[0] {
						method, path = o.Method, effectivePath(o.Path)
						break
					}
				}
				if path == "" {
					return errors.New("unknown operation ID; see api operations")
				}
			}
			p, e := pairs(params)
			if e != nil {
				return e
			}
			if p.Get("account_id") == "" && opt.account != "" {
				p.Set("account_id", opt.account)
			}
			for k, v := range p {
				path = strings.ReplaceAll(path, "{"+k+"}", url.PathEscape(v[0]))
			}
			if strings.Contains(path, "{") {
				return fmt.Errorf("unresolved path parameter in %s; use --param name=value", path)
			}
			q, e := pairs(queries)
			if e != nil {
				return e
			}
			b, ct, e := bodyInput(data, forms, files)
			if e != nil {
				return e
			}
			v, e := request(c.Context(), method, path, q, b, ct)
			if e != nil {
				return e
			}
			return emit(v)
		}}
		if raw {
			cmd.Use = "raw METHOD PATH"
			cmd.Args = cobra.ExactArgs(2)
		}
		cmd.Flags().StringArrayVar(&params, "param", nil, "Path parameter key=value (repeatable)")
		cmd.Flags().StringArrayVar(&queries, "query", nil, "Query key=value (repeatable)")
		cmd.Flags().StringVar(&data, "data", "", "JSON, @file or - for stdin")
		cmd.Flags().StringArrayVar(&forms, "form", nil, "Multipart text field=value (repeatable)")
		cmd.Flags().StringArrayVar(&files, "file", nil, "Multipart field=path (repeatable)")
		api.AddCommand(cmd)
	}
	return api
}

// Known upstream documentation typo, verified against canonical message route.
func effectivePath(path string) string {
	if path == "/accounts/{account_id}/conversations/{conversation_id}/messages" {
		return "/api/v1" + path
	}
	return path
}
