package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var emailRE = regexp.MustCompile(`(?i)\b[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}\b`)
var appIDRE = regexp.MustCompile(`(?i)\b[0-9a-f]{40}\b`)

func str(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil && fmt.Sprint(v) != "" {
			return fmt.Sprint(v)
		}
	}
	return ""
}
func number(m map[string]any, keys ...string) int64 {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			n, _ := strconv.ParseInt(fmt.Sprint(v), 10, 64)
			return n
		}
	}
	return 0
}
func mapValue(v any, keys ...string) any {
	m, _ := v.(map[string]any)
	if m == nil {
		return nil
	}
	for _, k := range keys {
		if x, ok := m[k]; ok {
			return x
		}
	}
	return nil
}
func listValue(v any) []any {
	if x, ok := v.([]any); ok {
		return x
	}
	if m, ok := v.(map[string]any); ok {
		for _, k := range []string{"payload", "data", "users", "items", "cvms"} {
			if x, ok := m[k].([]any); ok {
				return x
			}
			if d, ok := m[k].(map[string]any); ok {
				if x, ok := d["items"].([]any); ok {
					return x
				}
			}
		}
		if m["username"] != nil || m["hash_id"] != nil {
			return []any{m}
		}
	}
	return nil
}
func boolValue(v any) bool {
	b, _ := strconv.ParseBool(fmt.Sprint(v))
	return b || fmt.Sprint(v) == "1"
}
func isCustomerMessage(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	mt := fmt.Sprint(m["message_type"])
	return (mt == "0" || strings.EqualFold(mt, "incoming")) && !boolValue(m["private"])
}
func extractBriefMessages(v any) []any {
	out := []any{}
	for _, x := range listValue(v) {
		if isCustomerMessage(x) {
			out = append(out, x)
		}
	}
	return out
}

func conversationSender(m map[string]any) map[string]any {
	if x, ok := m["sender"].(map[string]any); ok {
		return x
	}
	if x, ok := m["meta"].(map[string]any); ok {
		if s, ok := x["sender"].(map[string]any); ok {
			return s
		}
	}
	return nil
}
func briefInbox(m map[string]any) any {
	if x := mapValue(m, "inbox"); x != nil {
		return x
	}
	if x := mapValue(m, "inbox_id"); x != nil {
		return map[string]any{"id": x}
	}
	return nil
}
func conversationAssignee(m map[string]any) any {
	if x, ok := m["assignee"].(map[string]any); ok {
		return x
	}
	if x, ok := m["meta"].(map[string]any); ok {
		return x["assignee"]
	}
	return nil
}
func briefOne(ctx context.Context, id string) (map[string]any, error) {
	v, err := callAccount(ctx, "GET", "/conversations/"+id, nil, nil)
	if err != nil {
		return nil, err
	}
	c, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("invalid conversation response")
	}
	msgs, err := messageHistory(ctx, id, "", true)
	if err != nil {
		return nil, err
	}
	customers := extractBriefMessages(msgs)
	emails, apps := []string{}, []string{}
	seenE, seenA := map[string]bool{}, map[string]bool{}
	for _, x := range customers {
		content := fmt.Sprint(mapValue(x, "content"))
		for _, e := range emailRE.FindAllString(content, -1) {
			e = strings.ToLower(e)
			if !seenE[e] {
				seenE[e] = true
				emails = append(emails, e)
			}
		}
		for _, a := range appIDRE.FindAllString(content, -1) {
			a = strings.ToLower(a)
			if !seenA[a] {
				seenA[a] = true
				apps = append(apps, a)
			}
		}
	}
	// Some email-channel messages omit the address from content; the sender is
	// still the customer's original address and is useful for account lookup.
	if sender := conversationSender(c); sender != nil {
		if e := strings.ToLower(str(sender, "email")); e != "" && !seenE[e] {
			emails = append(emails, e)
		}
	}
	out := map[string]any{"conversation": map[string]any{"id": id, "status": str(c, "status"), "assignee": conversationAssignee(c), "inbox": briefInbox(c), "sender": conversationSender(c)}, "customer_messages": customers, "emails": emails, "app_ids": apps}
	return out, nil
}
func briefText(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func briefPerson(v any) string {
	m, _ := v.(map[string]any)
	if m == nil {
		return briefText(v)
	}
	name := str(m, "name", "display_name", "displayName", "username")
	email := str(m, "email")
	if name != "" && email != "" {
		return name + " <" + email + ">"
	}
	if name != "" {
		return name
	}
	return email
}

func briefDate(m map[string]any) string {
	d := str(m, "created_at", "createdAt", "timestamp", "sent_at", "sentAt")
	if n, err := strconv.ParseInt(d, 10, 64); err == nil && n > 0 {
		return time.Unix(n, 0).UTC().Format("2006-01-02 15:04")
	}
	return d
}

func briefRenderOne(v map[string]any) {
	c, _ := v["conversation"].(map[string]any)
	id := str(c, "id")
	if id == "" {
		id = briefText(v["id"])
	}
	assignee := briefPerson(c["assignee"])
	inbox := briefText(mapValue(c, "inbox", "inbox_id"))
	if im, ok := c["inbox"].(map[string]any); ok {
		inbox = briefText(mapValue(im, "id", "name"))
	}
	fmt.Printf("#%s · %s · assignee: %s · inbox %s\n", id, str(c, "status"), assignee, inbox)
	fmt.Printf("Sender: %s\n\n", briefPerson(c["sender"]))
	fmt.Println("== Customer messages ==")
	msgs, _ := v["customer_messages"].([]any)
	ordered := append([]any(nil), msgs...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, _ := ordered[i].(map[string]any)
		b, _ := ordered[j].(map[string]any)
		ad, bd := briefDate(a), briefDate(b)
		if ad == bd {
			return number(a, "id") < number(b, "id")
		}
		return ad < bd
	})
	for _, x := range ordered {
		m, _ := x.(map[string]any)
		fmt.Printf("[%s] %s\n", briefDate(m), briefText(mapValue(m, "content")))
	}
	fmt.Println("\n== Extracted ==")
	emails, _ := v["emails"].([]string)
	apps, _ := v["app_ids"].([]string)
	if len(emails) == 0 {
		fmt.Println("Emails: none")
	} else {
		fmt.Println("Emails: " + strings.Join(emails, ", "))
	}
	if len(apps) == 0 {
		fmt.Println("App IDs: none")
	} else {
		fmt.Println("App IDs: " + strings.Join(apps, ", "))
	}
}

func briefRenderList(rows []any) {
	for _, x := range rows {
		m, _ := x.(map[string]any)
		preview := strings.Join(strings.Fields(briefText(m["preview"])), " ")
		fmt.Printf("#%s %s — %s [msg:%s]\n", briefText(m["id"]), briefPerson(m["sender"]), preview, briefText(m["last_customer_message_id"]))
	}
}

func addBrief(r *cobra.Command) {
	briefCmd := &cobra.Command{Use: "brief [CONVERSATION_ID]", Short: "Show a support brief, or unread open conversations assigned to Leechael", Args: cobra.MaximumNArgs(1), RunE: func(c *cobra.Command, a []string) error {
		if len(a) == 1 {
			if _, e := strconv.ParseUint(a[0], 10, 64); e != nil {
				return errors.New("conversation ID must be a positive integer")
			}
			v, e := briefOne(c.Context(), a[0])
			if e != nil {
				return e
			}
			if !opt.json && !opt.plain && opt.jq == "" {
				briefRenderOne(v)
			} else if e := emit(v); e != nil {
				return e
			}
			return nil
		}
		v, e := callAccount(c.Context(), "GET", "/conversations", url.Values{"status": {"open"}, "assignee_type": {"me"}}, nil)
		if e != nil {
			return e
		}
		rows := []any{}
		for _, x := range payload(v) {
			m, ok := x.(map[string]any)
			if !ok || number(m, "unread_count", "unread") <= 0 {
				continue
			}
			// Server-side assignee filter is unreliable; enforce client-side.
			meta, _ := m["meta"].(map[string]any)
			asg, _ := meta["assignee"].(map[string]any)
			if asg == nil || number(asg, "id") != 6 {
				continue
			}
			id := fmt.Sprint(m["id"])
			h, e := messageHistory(c.Context(), id, "", true)
			if e != nil {
				return e
			}
			cm := extractBriefMessages(h)
			if len(cm) == 0 {
				continue
			}
			last := cm[len(cm)-1]
			content := fmt.Sprint(mapValue(last, "content"))
			if len([]rune(content)) > 100 {
				content = string([]rune(content)[:100])
			}
			rows = append(rows, map[string]any{"id": id, "sender": conversationSender(m), "last_customer_message_id": mapValue(last, "id"), "preview": content})
		}
		sort.Slice(rows, func(i, j int) bool {
			return fmt.Sprint(rows[i].(map[string]any)["id"]) < fmt.Sprint(rows[j].(map[string]any)["id"])
		})
		if !opt.json && !opt.plain && opt.jq == "" {
			briefRenderList(rows)
			return nil
		}
		return emit(rows)
	}}
	r.AddCommand(briefCmd)
}
