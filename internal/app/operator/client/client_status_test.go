package operatorclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientActivity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/activity" || r.URL.Query().Get("workspace") != "dev" {
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"scope":{"kind":"workspace","workspace":"dev"},"exchanges":[{"exchange_id":"ex-1","workspace":"dev"}],"mcp_operations":[{"operation_id":"mcp-1","workspace":"dev","source":"docs","tool":"write","outcome":"failed","execution_possibility":"may_have_occurred"}]}`))
	}))
	defer server.Close()
	out, err := New(server.Client(), server.URL).Activity(context.Background(), "dev")
	if err != nil || len(out.Exchanges) != 1 || out.Exchanges[0].ExchangeID != "ex-1" {
		t.Fatalf("activity = %#v, err=%v", out, err)
	}
	if len(out.MCPOperations) != 1 || out.MCPOperations[0].Source != "docs" || out.MCPOperations[0].Occurrence != "may_have_occurred" {
		t.Fatalf("direct MCP activity=%#v", out.MCPOperations)
	}
}
