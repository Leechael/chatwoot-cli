package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/spf13/cobra"
	"io"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
)

func payload(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	if m, ok := v.(map[string]any); ok {
		if p, ok := m["payload"].([]any); ok {
			return p
		}
		if d, ok := m["data"].(map[string]any); ok {
			if p, ok := d["payload"].([]any); ok {
				return p
			}
		}
	}
	return nil
}
func idOf(v any) int64 {
	m, ok := v.(map[string]any)
	if !ok {
		return 0
	}
	n, _ := strconv.ParseInt(fmt.Sprint(m["id"]), 10, 64)
	return n
}
func messageHistory(ctx context.Context, id, before string, all bool) (any, error) {
	result := []any{}
	seen := map[int64]bool{}
	for page := 0; page < 10000; page++ {
		q := url.Values{}
		if before != "" {
			q.Set("before", before)
		}
		v, e := callAccount(ctx, "GET", "/conversations/"+id+"/messages", q, nil)
		if e != nil {
			return nil, e
		}
		if !all {
			return v, nil
		}
		items := payload(v)
		if items == nil {
			return nil, errors.New("unrecognized messages payload; refusing incomplete history")
		}
		if len(items) == 0 {
			sort.Slice(result, func(i, j int) bool { return idOf(result[i]) < idOf(result[j]) })
			return result, nil
		}
		min := int64(0)
		added := 0
		for _, m := range items {
			n := idOf(m)
			if n == 0 {
				return nil, errors.New("message missing numeric ID")
			}
			if min == 0 || n < min {
				min = n
			}
			if !seen[n] {
				seen[n] = true
				result = append(result, m)
				added++
			}
		}
		if added == 0 {
			return nil, errors.New("message pagination made no progress")
		}
		before = strconv.FormatInt(min, 10)
	}
	return nil, errors.New("message pagination exceeded 10000 pages")
}
func addTickets(r *cobra.Command) {
	r.AddCommand(&cobra.Command{Use: "status", Short: "Check authentication and show current profile", Args: cobra.NoArgs, RunE: func(c *cobra.Command, a []string) error {
		v, e := request(c.Context(), "GET", "/api/v1/profile", nil, nil, "")
		if e != nil {
			return e
		}
		return emit(v)
	}})
	var status, inbox, assignee string
	var page int
	var all bool
	list := &cobra.Command{Use: "list", Short: "List and filter conversations", Aliases: []string{"ls"}, Args: cobra.NoArgs, RunE: func(c *cobra.Command, a []string) error {
		if page < 1 {
			return errors.New("page must be >=1")
		}
		results := []any{}
		seen := map[int64]bool{}
		for n := 0; n < 10000; n++ {
			q := url.Values{"page": {strconv.Itoa(page + n)}}
			for k, v := range map[string]string{"status": status, "inbox_id": inbox, "assignee_type": assignee} {
				if v != "" {
					q.Set(k, v)
				}
			}
			v, e := callAccount(c.Context(), "GET", "/conversations", q, nil)
			if e != nil {
				return e
			}
			if !all {
				return emit(v)
			}
			items := payload(v)
			if items == nil {
				return errors.New("unrecognized conversation payload")
			}
			if len(items) == 0 {
				return emit(results)
			}
			added := 0
			for _, x := range items {
				id := idOf(x)
				if id == 0 {
					return errors.New("conversation missing ID")
				}
				if !seen[id] {
					seen[id] = true
					results = append(results, x)
					added++
				}
			}
			if added == 0 {
				return errors.New("conversation pagination made no progress")
			}
		}
		return errors.New("conversation pagination exceeded limit")
	}}
	list.Flags().StringVar(&status, "status", "", "open, resolved, pending, snoozed or all")
	list.Flags().StringVar(&inbox, "inbox-id", "", "Inbox ID")
	list.Flags().StringVar(&assignee, "assignee-type", "", "me, unassigned or all")
	list.Flags().IntVar(&page, "page", 1, "Page number")
	list.Flags().BoolVar(&all, "all", false, "Fetch all pages")
	r.AddCommand(list)
	r.AddCommand(&cobra.Command{Use: "get CONVERSATION_ID", Short: "Show conversation and complete message history", Args: conversationArgs(1), RunE: func(c *cobra.Command, a []string) error {
		v, e := callAccount(c.Context(), "GET", "/conversations/"+a[0], nil, nil)
		if e != nil {
			return e
		}
		m, e := messageHistory(c.Context(), a[0], "", true)
		if e != nil {
			return e
		}
		return emit(map[string]any{"conversation": v, "messages": m})
	}})
	var before string
	var allMessages bool
	messages := &cobra.Command{Use: "messages CONVERSATION_ID", Short: "Read conversation messages", Args: conversationArgs(1), RunE: func(c *cobra.Command, a []string) error {
		v, e := messageHistory(c.Context(), a[0], before, allMessages)
		if e != nil {
			return e
		}
		return emit(v)
	}}
	messages.Flags().StringVar(&before, "before", "", "Message ID cursor")
	messages.Flags().BoolVar(&allMessages, "all", false, "Fetch complete history")
	r.AddCommand(messages)
	var internal bool
	var attachments []string
	reply := &cobra.Command{Use: "reply CONVERSATION_ID [TEXT]", Short: "Send a reply, internal note or attachments", Args: conversationArgs(-1), RunE: func(c *cobra.Command, a []string) error {
		text := strings.Join(a[1:], " ")
		if len(a) == 1 {
			info, e := os.Stdin.Stat()
			if e != nil {
				return e
			}
			if info.Mode()&os.ModeCharDevice == 0 {
				b, e := io.ReadAll(os.Stdin)
				if e != nil {
					return e
				}
				text = string(b)
			}
		}
		if strings.TrimSpace(text) == "" && len(attachments) == 0 {
			return errors.New("reply requires text, piped stdin or attachment")
		}
		if len(attachments) == 0 {
			v, e := callAccount(c.Context(), "POST", "/conversations/"+a[0]+"/messages", nil, map[string]any{"content": text, "message_type": "outgoing", "private": internal})
			if e != nil {
				return e
			}
			return emit(v)
		}
		files := []string{}
		for _, f := range attachments {
			files = append(files, "attachments[]="+f)
		}
		b, ct, e := bodyInput("", []string{"content=" + text, "message_type=outgoing", "private=" + strconv.FormatBool(internal)}, files)
		if e != nil {
			return e
		}
		path, e := accountPath("/conversations/" + a[0] + "/messages")
		if e != nil {
			return e
		}
		v, e := request(c.Context(), "POST", path, nil, b, ct)
		if e != nil {
			return e
		}
		postReplyActions(c, a[0], internal, noMarkRead, resolve, assignAgent)
		return emit(v)
	}}
	reply.Flags().BoolVar(&internal, "internal", false, "Send a private/internal note")
	reply.Flags().StringVar(&replyFile, "file", "", "Read reply text from file (TEXT takes priority)")
	reply.Flags().BoolVar(&resolve, "resolve", false, "Resolve the conversation after sending")
	reply.Flags().StringVar(&assignAgent, "assign", "", "Assign the conversation to an agent name after sending")
	reply.Flags().BoolVar(&noMarkRead, "no-mark-read", false, "Do not mark the conversation read after sending")
	reply.Flags().StringArrayVar(&attachments, "attachment", nil, "Attach file (repeatable)")
	r.AddCommand(reply)
	markReadCmd := &cobra.Command{Use: "mark-read CONVERSATION_ID...", Short: "Mark one or more conversations as read", Args: func(c *cobra.Command, a []string) error {
		if err := cobra.MinimumNArgs(1)(c, a); err != nil {
			return err
		}
		for _, id := range a {
			if err := validateConversationID(id); err != nil {
				return err
			}
		}
		return nil
	}, RunE: func(c *cobra.Command, a []string) error {
		results := make([]any, 0, len(a))
		for _, id := range a {
			if err := markRead(c.Context(), id); err != nil {
				return err
			}
			results = append(results, map[string]any{"conversation_id": id, "marked_read": true})
		}
		if !opt.json && !opt.plain && opt.jq == "" {
			for _, id := range a {
				fmt.Printf("conversation %s marked read\n", id)
			}
			return nil
		}
		if len(results) == 1 {
			return emit(results[0])
		}
		return emit(results)
	}}
	r.AddCommand(markReadCmd)
	r.AddCommand(&cobra.Command{Use: "update-status CONVERSATION_ID STATUS", Short: "Set conversation status", Args: conversationArgs(2), RunE: func(c *cobra.Command, a []string) error {
		if !strings.Contains(" open resolved pending snoozed ", " "+a[1]+" ") {
			return errors.New("invalid status")
		}
		v, e := callAccount(c.Context(), "POST", "/conversations/"+a[0]+"/toggle_status", nil, map[string]any{"status": a[1]})
		if e != nil {
			return e
		}
		return emit(v)
	}})
	var team string
	assign := &cobra.Command{Use: "assign CONVERSATION_ID AGENT_ID_OR_NAME", Short: "Assign a conversation to an agent and optional team", Args: conversationArgs(2), RunE: func(c *cobra.Command, a []string) error {
		id, e := strconv.Atoi(a[1])
		if e != nil {
			v, e := callAccount(c.Context(), "GET", "/agents", nil, nil)
			if e != nil {
				return e
			}
			matches := []int{}
			for _, x := range payload(v) {
				m, ok := x.(map[string]any)
				if ok && strings.EqualFold(fmt.Sprint(m["name"]), a[1]) {
					matches = append(matches, int(idOf(m)))
				}
			}
			if len(matches) != 1 {
				return errors.New("agent name must match exactly one agent; use numeric ID")
			}
			id = matches[0]
		}
		if id < 0 {
			return errors.New("agent ID cannot be negative")
		}
		body := map[string]any{"assignee_id": id}
		if team != "" {
			n, e := strconv.Atoi(team)
			if e != nil {
				return e
			}
			if n < 0 {
				return errors.New("team ID cannot be negative")
			}
			body["team_id"] = n
		}
		v, e := callAccount(c.Context(), "POST", "/conversations/"+a[0]+"/assignments", nil, body)
		if e != nil {
			return e
		}
		return emit(v)
	}}
	assign.Flags().StringVar(&team, "team-id", "", "Also assign team ID")
	r.AddCommand(assign)
	r.AddCommand(&cobra.Command{Use: "search QUERY", Short: "Search account contacts by name, email, identifier or phone", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, a []string) error {
		v, e := callAccount(c.Context(), "GET", "/contacts/search", url.Values{"q": {a[0]}}, nil)
		if e != nil {
			return e
		}
		return emit(v)
	}})
	for _, x := range []struct{ name, path string }{{"integrations", "/inboxes"}, {"list-members", "/agents"}, {"contacts", "/contacts"}} {
		x := x
		r.AddCommand(&cobra.Command{Use: x.name, Args: cobra.NoArgs, RunE: func(c *cobra.Command, a []string) error {
			v, e := callAccount(c.Context(), "GET", x.path, nil, nil)
			if e != nil {
				return e
			}
			return emit(v)
		}})
	}

}

func validateConversationID(id string) error {
	value, err := strconv.ParseUint(id, 10, 64)
	if err != nil || value == 0 {
		return errors.New("conversation ID must be a positive integer")
	}
	return nil
}

func postReplyActions(c *cobra.Command, conversationID string, internal, noMarkRead, resolve bool, assignAgent string) {
	if !internal && !noMarkRead {
		if e := markRead(c.Context(), conversationID); e != nil {
			fmt.Fprintf(os.Stderr, "warning: reply sent but marking conversation %s read failed: %v\n", conversationID, e)
		}
	}
	if resolve {
		if _, e := callAccount(c.Context(), "POST", "/conversations/"+conversationID+"/toggle_status", nil, map[string]any{"status": "resolved"}); e != nil {
			fmt.Fprintf(os.Stderr, "warning: reply sent but resolving conversation %s failed: %v\n", conversationID, e)
		}
	}
	if assignAgent != "" {
		if e := assignConversation(c.Context(), conversationID, assignAgent); e != nil {
			fmt.Fprintf(os.Stderr, "warning: reply sent but assigning conversation %s failed: %v\n", conversationID, e)
		}
	}
}

func assignConversation(ctx context.Context, conversationID, agent string) error {
	id, e := strconv.Atoi(agent)
	if e != nil {
		v, e := callAccount(ctx, "GET", "/agents", nil, nil)
		if e != nil {
			return e
		}
		matches := []int{}
		for _, x := range payload(v) {
			m, ok := x.(map[string]any)
			if ok && strings.EqualFold(fmt.Sprint(m["name"]), agent) {
				matches = append(matches, int(idOf(m)))
			}
		}
		if len(matches) != 1 {
			return errors.New("agent name must match exactly one agent; use numeric ID")
		}
		id = matches[0]
	}
	if id < 0 {
		return errors.New("agent ID cannot be negative")
	}
	_, e = callAccount(ctx, "POST", "/conversations/"+conversationID+"/assignments", nil, map[string]any{"assignee_id": id})
	return e
}

func markRead(ctx context.Context, id string) error {
	_, err := callAccount(ctx, "POST", "/conversations/"+id+"/update_last_seen", nil, nil)
	return err
}

func conversationArgs(count int) cobra.PositionalArgs {
	return func(c *cobra.Command, a []string) error {
		if count < 0 {
			if err := cobra.MinimumNArgs(1)(c, a); err != nil {
				return err
			}
		} else if err := cobra.ExactArgs(count)(c, a); err != nil {
			return err
		}
		return validateConversationID(a[0])
	}
}
