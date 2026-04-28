package anthropic

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	anthropicadapter "github.com/metrofun/swobu/internal/adapters/outbound/providers/anthropic"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/internal/ports"
)

func TestAnthropicAdapter_MessagesContract(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotMethod string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","model":"claude-3-7-sonnet","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`))
	}))
	defer upstream.Close()

	executor := anthropicadapter.NewExecutor(upstream.Client(), staticConformanceResolver("token-123"))
	resp, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewDialogRequest("claude-3-7-sonnet", []compatibility.CanonicalItem{
			compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
		}), ports.NewExecutionContract(compatibility.DeliveryModeBuffered),
		ports.NewRoutableTarget("backend-a", "anthropic", upstream.URL+"/v1", "cred-1", protocolsurface.Messages, "", ""),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if resp.Output() == nil {
		t.Fatal("output = nil, want canonical output")
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want %q", gotMethod, http.MethodPost)
	}
	if gotPath != "/v1/messages" {
		t.Fatalf("path = %q, want %q", gotPath, "/v1/messages")
	}
}

func TestAnthropicAdapter_MessagesStreamingFirstByte(t *testing.T) {
	t.Parallel()

	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_1","model":"claude-3-7-sonnet"}}`,
			``,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"o"}}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}, "\n")))
	}))
	defer upstream.Close()

	executor := anthropicadapter.NewExecutor(upstream.Client(), staticConformanceResolver("token-123"))
	resp, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewDialogRequest("claude-3-7-sonnet", []compatibility.CanonicalItem{
			compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
		}), ports.NewExecutionContract(compatibility.DeliveryModeStreaming),
		ports.NewRoutableTarget("backend-a", "anthropic", upstream.URL+"/v1", "cred-1", protocolsurface.Messages, "", ""),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if gotPath != "/v1/messages" {
		t.Fatalf("path = %q, want %q", gotPath, "/v1/messages")
	}
	first, err := resp.Stream().Next()
	if err != nil {
		t.Fatalf("stream first event returned error: %v", err)
	}
	if first.Kind != compatibility.OutputEventStarted {
		t.Fatalf("first event kind = %q, want %q", first.Kind, compatibility.OutputEventStarted)
	}
}

func TestAnthropicAdapter_FailFastUnsupportedProtocolKind(t *testing.T) {
	t.Parallel()

	executor := anthropicadapter.NewExecutor(http.DefaultClient, staticConformanceResolver("token-123"))
	_, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewDialogRequest("claude-3-7-sonnet", []compatibility.CanonicalItem{
			compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
		}), ports.NewExecutionContract(compatibility.DeliveryModeBuffered),
		ports.NewRoutableTarget("backend-a", "anthropic", "https://example.test/v1", "cred-1", protocolsurface.ChatCompletions, "", ""),
	))
	if err == nil {
		t.Fatal("Execute returned nil error, want fail-fast error")
	}
	var swobuErr compatibility.Error
	if !errors.As(err, &swobuErr) {
		t.Fatalf("error type = %T, want compatibility.Error", err)
	}
	if swobuErr.Code != compatibility.ErrorCodeUnsupportedOperation {
		t.Fatalf("error code = %q, want %q", swobuErr.Code, compatibility.ErrorCodeUnsupportedOperation)
	}
}

func TestAnthropicAdapter_MessagesBufferedUsageReplay(t *testing.T) {
	t.Parallel()

	fixture := mustReadRuntimeFixture(t, "anthropic_messages_buffered_usage.json")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer upstream.Close()

	executor := anthropicadapter.NewExecutor(upstream.Client(), staticConformanceResolver("token-123"))
	resp, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewDialogRequest("claude-3-7-sonnet", []compatibility.CanonicalItem{
			compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
		}), ports.NewExecutionContract(compatibility.DeliveryModeBuffered),
		ports.NewRoutableTarget("backend-a", "anthropic", upstream.URL+"/v1", "cred-1", protocolsurface.Messages, "", ""),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	got := resp.Output()
	if got == nil {
		t.Fatal("output = nil, want canonical output")
	}
	assertUsage(t, got.Usage(), 10, 4, 0, 0)
}

func TestAnthropicAdapter_MessagesStreamingTerminalUsageReplay(t *testing.T) {
	t.Parallel()

	fixture := mustReadRuntimeFixture(t, "anthropic_messages_stream_usage.sse")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(fixture)
	}))
	defer upstream.Close()

	executor := anthropicadapter.NewExecutor(upstream.Client(), staticConformanceResolver("token-123"))
	resp, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewDialogRequest("claude-3-7-sonnet", []compatibility.CanonicalItem{
			compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
		}), ports.NewExecutionContract(compatibility.DeliveryModeStreaming),
		ports.NewRoutableTarget("backend-a", "anthropic", upstream.URL+"/v1", "cred-1", protocolsurface.Messages, "", ""),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	completed, doneErr := readCompletedEvent(resp.Stream())
	if doneErr != nil {
		t.Fatalf("read completed event: %v", doneErr)
	}
	assertUsage(t, completed.Usage, 10, 4, 0, 0)
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

func readCompletedEvent(stream compatibility.CanonicalOutputEventStream) (compatibility.OutputEvent, error) {
	for {
		event, err := stream.Next()
		if err != nil {
			return compatibility.OutputEvent{}, err
		}
		if event.Kind == compatibility.OutputEventCompleted {
			return event, nil
		}
	}
}

func assertUsage(t *testing.T, usage compatibility.TokenUsage, wantInput int, wantOutput int, wantCacheRead int, wantCacheWrite int) {
	t.Helper()
	if gotInput, ok := usage.InputTokens(); !ok || gotInput != wantInput {
		t.Fatalf("input tokens = (%d,%v), want (%d,true)", gotInput, ok, wantInput)
	}
	if gotOutput, ok := usage.OutputTokens(); !ok || gotOutput != wantOutput {
		t.Fatalf("output tokens = (%d,%v), want (%d,true)", gotOutput, ok, wantOutput)
	}
	if gotCacheRead, ok := usage.CacheReadTokens(); !ok || gotCacheRead != wantCacheRead {
		t.Fatalf("cache read tokens = (%d,%v), want (%d,true)", gotCacheRead, ok, wantCacheRead)
	}
	if gotCacheWrite, ok := usage.CacheWriteTokens(); !ok || gotCacheWrite != wantCacheWrite {
		t.Fatalf("cache write tokens = (%d,%v), want (%d,true)", gotCacheWrite, ok, wantCacheWrite)
	}
}

type staticConformanceResolver string

func (r staticConformanceResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return string(r), nil
}
