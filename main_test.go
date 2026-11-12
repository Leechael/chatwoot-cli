package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setup(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	opt = options{base: s.URL, token: "app-test", platform: "platform-test", account: "1", auth: "api-key", timeout: time.Second}
}
func TestCatalogCoverage(t *testing.T) {
	ops := operations()
	if len(ops) != 157 {
		t.Fatalf("catalog drift: %d", len(ops))
	}
	seen := map[string]bool{}
	for _, o := range ops {
		if o.ID == "" || seen[o.ID] {
			t.Fatal("missing/duplicate operation", o.ID)
		}
		seen[o.ID] = true
	}
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/public/") || strings.HasPrefix(r.URL.Path, "/survey/") {
			if r.Header.Get("api_access_token") != "" {
				t.Error("token leaked")
			}
		} else if strings.HasPrefix(r.URL.Path, "/platform/") {
			if r.Header.Get("api_access_token") != "platform-test" {
				t.Error("wrong platform token")
			}
		} else if r.Header.Get("api_access_token") != "app-test" {
			t.Error("missing app token")
		}
		fmt.Fprint(w, `{"ok":true}`)
	})
	for _, o := range ops {
		p := o.Path
		for strings.Contains(p, "{") {
			a := strings.Index(p, "{")
			b := strings.Index(p[a:], "}") + a
			p = p[:a] + "42" + p[b+1:]
		}
		if _, e := request(context.Background(), o.Method, p, nil, nil, ""); e != nil {
			t.Errorf("%s: %v", o.ID, e)
		}
	}
}
func TestMessagesPagination(t *testing.T) {
	n := 0
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		n++
		switch r.URL.Query().Get("before") {
		case "":
			fmt.Fprint(w, `{"payload":[{"id":3},{"id":4}]}`)
		case "3":
			fmt.Fprint(w, `{"payload":[{"id":1},{"id":2}]}`)
		case "1":
			fmt.Fprint(w, `{"payload":[]}`)
		default:
			t.Fatal(r.URL)
		}
	})
	v, e := messageHistory(context.Background(), "5", "", true)
	if e != nil {
		t.Fatal(e)
	}
	a := v.([]any)
	if len(a) != 4 || idOf(a[0]) != 1 || n != 3 {
		t.Fatalf("%v calls=%d", v, n)
	}
}
func TestNoProgress(t *testing.T) {
	setup(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"payload":[{"id":1}]}`) })
	_, e := messageHistory(context.Background(), "1", "", true)
	if e == nil || !strings.Contains(e.Error(), "no progress") {
		t.Fatal(e)
	}
}
func TestMultipart(t *testing.T) {
	f := filepath.Join(t.TempDir(), "test.txt")
	if e := os.WriteFile(f, []byte("real bytes"), 0600); e != nil {
		t.Fatal(e)
	}
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		if e := r.ParseMultipartForm(1 << 20); e != nil {
			t.Fatal(e)
		}
		file, _, e := r.FormFile("attachments[]")
		if e != nil {
			t.Fatal(e)
		}
		defer file.Close()
		b, _ := io.ReadAll(file)
		if string(b) != "real bytes" || r.FormValue("private") != "true" {
			t.Fatal("multipart mismatch")
		}
		fmt.Fprint(w, `{"id":1}`)
	})
	b, ct, e := bodyInput("", []string{"private=true"}, []string{"attachments[]=" + f})
	if e != nil {
		t.Fatal(e)
	}
	_, e = request(context.Background(), "POST", "/api/v1/test", nil, b, ct)
	if e != nil {
		t.Fatal(e)
	}
}
func TestHTTPErrorCodes(t *testing.T) {
	for _, x := range []struct{ status, code int }{{401, 2}, {403, 2}, {404, 3}, {429, 1}, {500, 1}} {
		t.Run(fmt.Sprint(x.status), func(t *testing.T) {
			setup(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(x.status)
				fmt.Fprint(w, `{"error":"failure"}`)
			})
			_, e := request(context.Background(), "GET", "/test", nil, nil, "")
			if e == nil || exitCode(e) != x.code {
				t.Fatal(e)
			}
		})
	}
}
func TestRedirectDoesNotLeak(t *testing.T) {
	called := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer destination.Close()
	setup(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 302) })
	_, e := request(context.Background(), "GET", "/test", nil, nil, "")
	if e == nil || called {
		t.Fatal("followed redirect")
	}
}
func TestQueryAndEscaping(t *testing.T) {
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/test/a%2Fb" || r.URL.Query().Get("q") != "a&b" {
			t.Fatal(r.URL)
		}
		fmt.Fprint(w, `{}`)
	})
	_, e := request(context.Background(), "GET", "/test/"+url.PathEscape("a/b"), url.Values{"q": {"a&b"}}, nil, "")
	if e != nil {
		t.Fatal(e)
	}
}
func TestBodyValidation(t *testing.T) {
	if _, _, e := bodyInput("not json", nil, nil); e == nil {
		t.Fatal("invalid json accepted")
	}
	if _, _, e := bodyInput(`{}`, []string{"a=b"}, nil); e == nil {
		t.Fatal("mixed bodies accepted")
	}
	b, _, e := bodyInput(`{"template_params":{"name":"x"},"content_attributes":{"in_reply_to":3}}`, nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := io.ReadAll(b)
	if !bytes.Contains(raw, []byte("template_params")) {
		t.Fatal("lost fields")
	}
}
func TestNonJSON(t *testing.T) {
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html>survey</html>")
	})
	v, e := request(context.Background(), "GET", "/survey/x", nil, nil, "")
	if e != nil || v.(map[string]any)["body"] != "<html>survey</html>" {
		t.Fatal(v, e)
	}
}
func TestBearer(t *testing.T) {
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer app-test" || r.Header.Get("api_access_token") != "" {
			t.Fatal("wrong bearer")
		}
		fmt.Fprint(w, `{}`)
	})
	opt.auth = "bearer"
	_, e := request(context.Background(), "GET", "/test", nil, nil, "")
	if e != nil {
		t.Fatal(e)
	}
}
func TestAgentsCommandDefaultsToAccountOne(t *testing.T) {
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/accounts/1/agents" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, `[{"id":7,"name":"Ada","email":"ada@example.com","role":"administrator"}]`)
	})
	opt.account = ""
	base := opt.base
	r := root()
	r.SetArgs([]string{"--base-url", base, "--token", "app-test", "--json", "agents"})
	if e := r.Execute(); e != nil {
		t.Fatal(e)
	}
}

func TestAgentsCommandUsesAccountID(t *testing.T) {
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/accounts/42/agents" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		fmt.Fprint(w, `[]`)
	})
	base := opt.base
	r := root()
	r.SetArgs([]string{"--base-url", base, "--token", "app-test", "--account-id", "42", "agents"})
	if e := r.Execute(); e != nil {
		t.Fatal(e)
	}
}

func TestReplyCommand(t *testing.T) {
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/accounts/1/conversations/9/messages" || r.Method != "POST" {
			t.Fatal(r.URL)
		}
		var b map[string]any
		if e := json.NewDecoder(r.Body).Decode(&b); e != nil {
			t.Fatal(e)
		}
		if b["content"] != "hello" || b["private"] != true || b["message_type"] != "outgoing" {
			t.Fatal(b)
		}
		fmt.Fprint(w, `{"id":1}`)
	})
	base := opt.base
	r := root()
	r.SetArgs([]string{"--base-url", base, "--token", "app-test", "--account-id", "1", "reply", "9", "hello", "--internal"})
	if e := r.Execute(); e != nil {
		t.Fatal(e)
	}
}

func TestMarkReadCommand(t *testing.T) {
	calls := []string{}
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/update_last_seen") {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		calls = append(calls, r.URL.Path)
		fmt.Fprint(w, `{}`)
	})
	base := opt.base
	r := root()
	r.SetArgs([]string{"--base-url", base, "--token", "app-test", "--account-id", "1", "--json", "mark-read", "1434", "1435"})
	if e := r.Execute(); e != nil {
		t.Fatal(e)
	}
	if len(calls) != 2 || !strings.HasSuffix(calls[0], "/conversations/1434/update_last_seen") || !strings.HasSuffix(calls[1], "/conversations/1435/update_last_seen") {
		t.Fatalf("mark-read calls: %v", calls)
	}
}

func TestReplyFileAndTextPriority(t *testing.T) {
	f := filepath.Join(t.TempDir(), "reply.txt")
	if e := os.WriteFile(f, []byte("from file"), 0600); e != nil {
		t.Fatal(e)
	}
	contents := []string{}
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		if e := json.NewDecoder(r.Body).Decode(&b); e != nil {
			t.Fatal(e)
		}
		contents = append(contents, b["content"].(string))
		fmt.Fprint(w, `{"id":1}`)
	})
	base := opt.base
	for _, args := range [][]string{
		{"--base-url", base, "--token", "app-test", "--account-id", "1", "reply", "9", "--file", f, "--no-mark-read"},
		{"--base-url", base, "--token", "app-test", "--account-id", "1", "reply", "9", "inline", "--file", f, "--no-mark-read"},
	} {
		r := root()
		r.SetArgs(args)
		if e := r.Execute(); e != nil {
			t.Fatal(e)
		}
	}
	if !strings.EqualFold(contents[0], "from file") || contents[1] != "inline" {
		t.Fatalf("contents: %v", contents)
	}
}

func TestReplyReadsStdin(t *testing.T) {
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/messages") {
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b["content"] != "from stdin" {
				t.Fatalf("body: %v", b)
			}
			fmt.Fprint(w, `{"id":1}`)
			return
		}
		fmt.Fprint(w, `{}`)
	})
	old := os.Stdin
	in, out, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	_, _ = out.WriteString("from stdin")
	_ = out.Close()
	os.Stdin = in
	t.Cleanup(func() { _ = in.Close(); os.Stdin = old })
	base := opt.base
	r := root()
	r.SetArgs([]string{"--base-url", base, "--token", "app-test", "--account-id", "1", "reply", "9"})
	if e := r.Execute(); e != nil {
		t.Fatal(e)
	}
}

func TestReplyResolveAndAssign(t *testing.T) {
	paths := []string{}
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch {
		case strings.HasSuffix(r.URL.Path, "/messages"):
			fmt.Fprint(w, `{"id":1}`)
		case strings.HasSuffix(r.URL.Path, "/update_last_seen"):
			fmt.Fprint(w, `{}`)
		case r.URL.Path == "/api/v1/accounts/1/conversations/9/toggle_status":
			fmt.Fprint(w, `{}`)
		case r.URL.Path == "/api/v1/accounts/1/agents":
			fmt.Fprint(w, `[{"id":7,"name":"Ada"}]`)
		case strings.HasSuffix(r.URL.Path, "/assignments"):
			fmt.Fprint(w, `{}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	base := opt.base
	r := root()
	r.SetArgs([]string{"--base-url", base, "--token", "app-test", "--account-id", "1", "reply", "9", "hello", "--resolve", "--assign", "Ada"})
	if e := r.Execute(); e != nil {
		t.Fatal(e)
	}
	if len(paths) != 5 || !strings.HasSuffix(paths[2], "/toggle_status") || !strings.HasSuffix(paths[4], "/assignments") {
		t.Fatalf("paths: %v", paths)
	}
}

func TestReplyAutomaticallyMarksRead(t *testing.T) {
	calls := []string{}
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/messages") {
			fmt.Fprint(w, `{"id":1}`)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/update_last_seen") {
			fmt.Fprint(w, `{}`)
			return
		}
		t.Fatalf("unexpected request: %s", r.URL.Path)
	})
	base := opt.base
	r := root()
	r.SetArgs([]string{"--base-url", base, "--token", "app-test", "--account-id", "1", "reply", "9", "hello"})
	if e := r.Execute(); e != nil {
		t.Fatal(e)
	}
	if len(calls) != 2 || !strings.HasSuffix(calls[1], "/conversations/9/update_last_seen") {
		t.Fatalf("reply calls: %v", calls)
	}
}

func TestReplyMarkReadFailureDoesNotFailReply(t *testing.T) {
	calls := 0
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if strings.HasSuffix(r.URL.Path, "/messages") {
			fmt.Fprint(w, `{"id":1}`)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"mark read failed"}`)
	})
	base := opt.base
	r := root()
	r.SetArgs([]string{"--base-url", base, "--token", "app-test", "--account-id", "1", "reply", "9", "hello"})
	if e := r.Execute(); e != nil {
		t.Fatal(e)
	}
	if calls != 2 {
		t.Fatalf("expected reply and warning-only mark-read calls, got %d", calls)
	}
}

func TestBriefMarkRead(t *testing.T) {
	seen := false
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/accounts/1/conversations/1434":
			fmt.Fprint(w, `{"id":1434,"status":"open","sender":{"name":"Customer"}}`)
		case "/api/v1/accounts/1/conversations/1434/messages":
			if r.URL.Query().Get("before") == "" {
				fmt.Fprint(w, `{"payload":[{"id":11182,"message_type":"incoming","content":"hello"}]}`)
			} else {
				fmt.Fprint(w, `{"payload":[]}`)
			}
		case "/api/v1/accounts/1/conversations/1434/update_last_seen":
			seen = true
			fmt.Fprint(w, `{}`)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	})
	base := opt.base
	r := root()
	r.SetArgs([]string{"--base-url", base, "--token", "app-test", "--account-id", "1", "brief", "1434", "--mark-read"})
	if e := r.Execute(); e != nil {
		t.Fatal(e)
	}
	if !seen {
		t.Fatal("brief did not mark conversation read")
	}
}

func TestBriefRenderListIncludesMessageID(t *testing.T) {
	old := os.Stdout
	out, in, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	os.Stdout = in
	briefRenderList([]any{map[string]any{"id": "1434", "sender": map[string]any{"name": "Customer"}, "preview": "hello", "last_customer_message_id": 11182}})
	_ = in.Close()
	os.Stdout = old
	b, _ := io.ReadAll(out)
	_ = out.Close()
	if !strings.Contains(string(b), "#1434 Customer — hello [msg:11182]") {
		t.Fatalf("output: %s", b)
	}
}

func TestCanonicalPathCorrection(t *testing.T) {
	old := "/accounts/{account_id}/conversations/{conversation_id}/messages"
	if effectivePath(old) != "/api/v1"+old {
		t.Fatal("missing upstream route correction")
	}
	if effectivePath("/api/v1/profile") != "/api/v1/profile" {
		t.Fatal("changed canonical route")
	}
}
func TestConversationIDValidation(t *testing.T) {
	for _, id := range []string{"0", "-1", "1/../2", "abc", ""} {
		if conversationArgs(1)(nil, []string{id}) == nil {
			t.Fatalf("accepted %q", id)
		}
	}
	if e := conversationArgs(1)(nil, []string{"123"}); e != nil {
		t.Fatal(e)
	}
}
func TestInheritedParameters(t *testing.T) {
	found := false
	for _, o := range operations() {
		if o.ID == "get-details-of-a-conversation" {
			_, found = o.Spec["x-path-parameters"]
		}
	}
	if !found {
		t.Fatal("inherited path parameters absent")
	}
}
func TestResponseLimit(t *testing.T) {
	setup(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(bytes.Repeat([]byte("x"), (64<<20)+1)) })
	opt.timeout = 15 * time.Second
	_, e := request(context.Background(), "GET", "/test", nil, nil, "")
	if e == nil || !strings.Contains(e.Error(), "exceeds") {
		t.Fatal(e)
	}
}

func TestGenericOperationCLI(t *testing.T) {
	setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/accounts/1/conversations/9/messages" || r.URL.Query().Get("before") != "22" {
			t.Fatal(r.URL)
		}
		fmt.Fprint(w, `{"payload":[]}`)
	})
	base := opt.base
	r := root()
	r.SetArgs([]string{"--base-url", base, "--token", "app-test", "--account-id", "1", "api", "call", "getConversationMessages", "--param", "conversation_id=9", "--query", "before=22"})
	if e := r.Execute(); e != nil {
		t.Fatal(e)
	}
}
func TestInvalidOutputModes(t *testing.T) {
	for _, args := range [][]string{{"--json", "--plain", "api", "operations"}, {"--jq", "length", "api", "operations"}} {
		r := root()
		r.SetArgs(args)
		if r.Execute() == nil {
			t.Fatal("accepted incompatible output mode")
		}
	}
}
