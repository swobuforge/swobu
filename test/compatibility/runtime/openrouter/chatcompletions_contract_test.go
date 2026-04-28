package openrouter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	customadapter "github.com/metrofun/swobu/internal/adapters/outbound/providers/custom"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/internal/ports"
)

func TestOpenRouterAdapter_ChatCompletionsBufferedUsageReplay(t *testing.T) {
	t.Parallel()

	fixture := mustReadRuntimeFixture(t, "openrouter_chat_buffered_usage.json")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer upstream.Close()

	executor := customadapter.NewExecutor(upstream.Client(), staticConformanceResolver("token-123"))
	resp, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewDialogRequest("openrouter/model", []compatibility.CanonicalItem{
			compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
		}), ports.NewExecutionContract(compatibility.DeliveryModeBuffered),
		ports.NewRoutableTarget("backend-openrouter", "openrouter", upstream.URL+"/v1", "cred-1", protocolsurface.ChatCompletions, "", ""),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if got := resp.Output(); got == nil {
		t.Fatal("output = nil, want canonical output")
	} else {
		assertUsage(t, got.Usage(), 10, 2, 0, 0)
	}
}

func assertUsage(t *testing.T, usage compatibility.TokenUsage, wantInput int, wantOutput int, wantCacheRead int, wantCacheWrite int) {
	t.Helper()

	gotInput, ok := usage.InputTokens()
	if !ok || gotInput != wantInput {
		t.Fatalf("input tokens = (%d,%v), want (%d,true)", gotInput, ok, wantInput)
	}
	gotOutput, ok := usage.OutputTokens()
	if !ok || gotOutput != wantOutput {
		t.Fatalf("output tokens = (%d,%v), want (%d,true)", gotOutput, ok, wantOutput)
	}
	gotCacheRead, ok := usage.CacheReadTokens()
	if !ok || gotCacheRead != wantCacheRead {
		t.Fatalf("cache read tokens = (%d,%v), want (%d,true)", gotCacheRead, ok, wantCacheRead)
	}
	gotCacheWrite, ok := usage.CacheWriteTokens()
	if !ok || gotCacheWrite != wantCacheWrite {
		t.Fatalf("cache write tokens = (%d,%v), want (%d,true)", gotCacheWrite, ok, wantCacheWrite)
	}
}

func mustReadRuntimeFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := "testdata/" + name
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return raw
}

type staticConformanceResolver string

func (r staticConformanceResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return string(r), nil
}
