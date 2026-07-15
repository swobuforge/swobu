package operatorclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientStatusProjection(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/_swobu/status-projection" {
			t.Fatalf("request method/path = %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("scope"); got != "all" {
			t.Fatalf("scope query = %q, want all", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"state":"running",
			"recent_traffic":[{
				"request_id":"req-1",
				"endpoint":"dev",
				"client_family":"codex",
				"route":"cfg-1:gpt-4.1",
				"result":"success",
				"status_code":200,
				"observed_at":"15:04:05",
				"model_requested":"gpt-4.1",
				"model_resolved":"gpt-4.1",
				"token_usage":{"input_tokens":120,"output_tokens":30},
				"exchange_stage_reports":[{
					"stage":"provider.wire.out",
					"carrier":"request",
					"applied":["openai.encode"],
					"mutated":true
				}],
				"exchange_diagnostics":["high_patch_noop_ratio:4/5"]
			}]
		}`))
	}))
	defer server.Close()

	c := New(server.Client(), server.URL)
	out, err := c.Status(context.Background(), "all")
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if out.State != "running" {
		t.Fatalf("state = %q, want running", out.State)
	}
	if len(out.RecentTraffic) != 1 {
		t.Fatalf("recent traffic rows = %d, want 1", len(out.RecentTraffic))
	}
	row := out.RecentTraffic[0]
	if row.Endpoint != "dev" || row.ClientFamily != "codex" || row.ModelResolved != "gpt-4.1" {
		t.Fatalf("unexpected row: %#v", row)
	}
	if row.TokenUsage == nil || row.TokenUsage.InputTokens == nil || *row.TokenUsage.InputTokens != 120 {
		t.Fatalf("token usage not decoded: %#v", row.TokenUsage)
	}
	if len(row.StageReports) != 1 || row.StageReports[0].Stage != "provider.wire.out" {
		t.Fatalf("stage reports not decoded: %#v", row.StageReports)
	}
	if len(row.ExchangeDiagnostics) != 1 {
		t.Fatalf("diagnostics not decoded: %#v", row.ExchangeDiagnostics)
	}
}

func TestClientStatusProjectionRequiresScope(t *testing.T) {
	t.Parallel()
	c := New(http.DefaultClient, "http://127.0.0.1:1")
	if _, err := c.Status(context.Background(), ""); err == nil {
		t.Fatal("expected missing scope error")
	}
}
