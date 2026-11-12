//go:build integration

package main

import (
	"context"
	"os"
	"testing"
	"time"
)

// Read-only acceptance against a real deployment. No writes, even when credentials exist.
func TestLiveReadOnly(t *testing.T) {
	base, token, account := os.Getenv("CHATWOOT_BASE_URL"), os.Getenv("CHATWOOT_API_TOKEN"), os.Getenv("CHATWOOT_ACCOUNT_ID")
	if base == "" || token == "" || account == "" {
		t.Skip("set CHATWOOT_BASE_URL, CHATWOOT_API_TOKEN and CHATWOOT_ACCOUNT_ID")
	}
	opt = options{base: base, token: token, account: account, auth: "api-key", timeout: 30 * time.Second}
	if _, e := request(context.Background(), "GET", "/api/v1/profile", nil, nil, ""); e != nil {
		t.Fatal(e)
	}
	if _, e := callAccount(context.Background(), "GET", "/conversations", nil, nil); e != nil {
		t.Fatal(e)
	}
	if id := os.Getenv("CHATWOOT_TEST_CONVERSATION_ID"); id != "" {
		if _, e := messageHistory(context.Background(), id, "", true); e != nil {
			t.Fatal(e)
		}
	}
}
