package operatorclient

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestActivityOperationBindingRedactionAndNoRetry(t *testing.T) {
	var calls atomic.Int32
	body := `{"kind":"mcp_operation","evidence":"retained","mcp_operation":{"operation_id":"mcp_exact","access_id":"local","workspace":"research","source":"docs","tool":"write","started_at":"2026-10-13T00:00:00Z","outcome":"failed","execution_possibility":"may_have_occurred","arguments":"private-fixture","result":"private-fixture"}}`
	node := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.URL.Path != "/api/v1/activity/operations/mcp_exact" || request.URL.RawQuery != "" {
			t.Error("detail escaped exact canonical path")
		}
		fmt.Fprint(writer, body)
	}))
	defer node.Close()
	client := New(node.Client(), node.URL)
	detail, err := client.ActivityOperation(t.Context(), "mcp_exact")
	if err != nil || detail.MCP == nil || detail.MCP.Occurrence != "may_have_occurred" {
		t.Fatal("detail lost uncertainty", err)
	}
	projected, _ := json.Marshal(detail)
	if strings.Contains(string(projected), "private-fixture") {
		t.Fatal("detail disclosed content")
	}
	for _, identity := range []string{"", "mcp_", "mcp_exact/other", "mcp_exact?admin=true", "ex_bad"} {
		before := calls.Load()
		if _, err := client.ActivityOperation(t.Context(), identity); err == nil || calls.Load() != before {
			t.Fatal("invalid identity reached Node")
		}
	}
	for _, invalid := range []string{strings.Replace(body, `"mcp_exact"`, `"mcp_foreign"`, 1), strings.Replace(body, `"retained"`, `"unknown"`, 1), body + `{}`, strings.TrimSuffix(body, "}") + `,"exchange":{}}`} {
		body = invalid
		before := calls.Load()
		if _, err := client.ActivityOperation(t.Context(), "mcp_exact"); err == nil || calls.Load() != before+1 {
			t.Fatal("invalid detail accepted or retried")
		}
	}
}

// The shared operator client is used by the CLI and agent surfaces, not just
// the WebUI. It must retain uncertainty, causal route and Connection revisions.
func TestActivityOperationPreservesCausalRecoveryAndRejectsFalseCertainty(t *testing.T) {
	const identity = "ex_AAAAAAAAAAAAAAAAAAAAAA"
	body := `{"kind":"exchange","evidence":"retained","exchange":{"exchange_id":"ex_AAAAAAAAAAAAAAAAAAAAAA","access_id":"local","workspace":"research","started_at":"2026-10-20T12:00:00Z","delivery_result":"client_write_failed","error_code":"internal","upstream_execution":{"recorded_invocations":2,"occurred":1,"may_have_occurred":1,"did_not_occur":0,"unsettled":0},"invocations":[{"invocation_id":"inv_AAAAAAAAAAAAAAAAAAAAAA_1","kind":"model","started_at":"2026-10-20T12:00:01Z","route":"coding","provider":"openai","model":"gpt-5","target":"azure","target_version":3,"connection_id":"cn_account","connection_revision":8,"execution_possibility":"may_have_occurred"},{"invocation_id":"inv_AAAAAAAAAAAAAAAAAAAAAA_2","kind":"mcp_tool","started_at":"2026-10-20T12:00:02Z","source":"github","tool":"create_issue","connection_id":"cn_mcp","connection_revision":5,"execution_possibility":"occurred"}]}}`
	var calls int
	node := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		if request.URL.Path != "/api/v1/activity/operations/"+identity {
			t.Errorf("escaped canonical Activity detail path: %s", request.URL.Path)
		}
		_, _ = fmt.Fprint(writer, body)
	}))
	defer node.Close()
	client := New(node.Client(), node.URL)
	detail, err := client.ActivityOperation(t.Context(), identity)
	if err != nil || detail.Exchange == nil {
		t.Fatalf("cannot read causal exchange: %v", err)
	}
	got := detail.Exchange
	if got.ErrorCode != "internal" || got.Result != "client_write_failed" || got.Upstream == nil || got.Upstream.Occurred != 1 || got.Upstream.MayHaveOccurred != 1 {
		t.Fatalf("delivery or uncertainty lost: %#v", got)
	}
	if len(got.Invocations) != 2 || got.Invocations[0].Route != "coding" || got.Invocations[0].ConnectionID != "cn_account" || got.Invocations[0].ConnectionRevision != 8 || got.Invocations[1].ConnectionRevision != 5 {
		t.Fatalf("causal provenance lost: %#v", got.Invocations)
	}
	for _, invalid := range []string{
		strings.Replace(body, `"may_have_occurred":1`, `"may_have_occurred":0`, 1),
		strings.Replace(body, `"upstream_execution":`, `"no_upstream_execution":`, 1),
		strings.Replace(body, `"may_have_occurred"`, `"invented_status"`, 1),
	} {
		body = invalid
		before := calls
		if _, err := client.ActivityOperation(t.Context(), identity); err == nil || calls != before+1 {
			t.Fatal("invalid Activity recovery summary was accepted or retried")
		}
		body = `{"kind":"exchange","evidence":"retained","exchange":{"exchange_id":"ex_AAAAAAAAAAAAAAAAAAAAAA","access_id":"local","workspace":"research","started_at":"2026-10-20T12:00:00Z","delivery_result":"client_write_failed","error_code":"internal","upstream_execution":{"recorded_invocations":2,"occurred":1,"may_have_occurred":1,"did_not_occur":0,"unsettled":0},"invocations":[{"invocation_id":"inv_AAAAAAAAAAAAAAAAAAAAAA_1","kind":"model","started_at":"2026-10-20T12:00:01Z","route":"coding","provider":"openai","model":"gpt-5","target":"azure","target_version":3,"connection_id":"cn_account","connection_revision":8,"execution_possibility":"may_have_occurred"},{"invocation_id":"inv_AAAAAAAAAAAAAAAAAAAAAA_2","kind":"mcp_tool","started_at":"2026-10-20T12:00:02Z","source":"github","tool":"create_issue","connection_id":"cn_mcp","connection_revision":5,"execution_possibility":"occurred"}]}}`
	}
}
