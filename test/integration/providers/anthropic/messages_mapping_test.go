package anthropic

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	anthropicadapter "github.com/metrofun/swobu/internal/adapters/outbound/providers/anthropic"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/internal/ports"
)

func TestAnthropicAdapter_MapsConversationRequestsToMessagesWire(t *testing.T) {
	t.Parallel()

	var gotMethod string
	var gotPath string
	var gotVersion string
	var gotAPIKey string
	var gotCallerUA string
	var gotBody string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotVersion = strings.TrimSpace(r.Header.Get("anthropic-version"))
		gotAPIKey = strings.TrimSpace(r.Header.Get("x-api-key"))
		gotCallerUA = strings.TrimSpace(r.Header.Get("User-Agent"))
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","model":"claude-3-7-sonnet","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`))
	}))
	defer upstream.Close()

	executor := anthropicadapter.NewExecutor(upstream.Client(), staticResolver("token-123"))
	_, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewDialogRequest("claude-3-7-sonnet", []compatibility.CanonicalItem{
			compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
			compatibility.NewToolUseItem(compatibility.ItemAuthorAssistant, "", "toolu_1", "grep", map[string]any{"pattern": "TODO"}),
			compatibility.NewToolResultItem(compatibility.ItemAuthorUser, "toolu_1", "2 hits"),
		}), ports.NewExecutionContract(compatibility.DeliveryModeBuffered),
		ports.NewRoutableTarget("backend-a", "anthropic", upstream.URL+"/v1", "cred-1", protocolsurface.Messages, "", ""),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q, want %q", gotMethod, http.MethodPost)
	}
	if gotPath != "/v1/messages" {
		t.Fatalf("path = %q, want %q", gotPath, "/v1/messages")
	}
	if gotVersion != "2023-06-01" {
		t.Fatalf("anthropic-version = %q, want %q", gotVersion, "2023-06-01")
	}
	if gotAPIKey != "token-123" {
		t.Fatalf("x-api-key = %q, want %q", gotAPIKey, "token-123")
	}
	if gotCallerUA != "swobu/dev" {
		t.Fatalf("user-agent = %q, want %q", gotCallerUA, "swobu/dev")
	}
	wantBody := `{"model":"claude-3-7-sonnet","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]},{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"grep","input":{"pattern":"TODO"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"2 hits"}]}],"max_tokens":256}`
	if gotBody != wantBody {
		t.Fatalf("body = %q, want %q", gotBody, wantBody)
	}
}

func TestAnthropicAdapter_DecodesBufferedMessagesOutput(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","model":"claude-3-7-sonnet","content":[{"type":"text","text":"hello"},{"type":"tool_use","id":"toolu_1","name":"grep","input":{"pattern":"TODO"}}],"stop_reason":"tool_use"}`))
	}))
	defer upstream.Close()

	executor := anthropicadapter.NewExecutor(upstream.Client(), staticResolver("token-123"))
	resp, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewDialogRequest("claude-3-7-sonnet", []compatibility.CanonicalItem{
			compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
		}), ports.NewExecutionContract(compatibility.DeliveryModeBuffered),
		ports.NewRoutableTarget("backend-a", "anthropic", upstream.URL+"/v1", "cred-1", protocolsurface.Messages, "", ""),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if got := resp.DeliveryMode(); got != compatibility.DeliveryModeBuffered {
		t.Fatalf("delivery mode = %q, want %q", got, compatibility.DeliveryModeBuffered)
	}
	typed, ok := resp.Output().(compatibility.CanonicalOutputValue)
	if !ok {
		t.Fatalf("output type = %T, want compatibility.CanonicalOutputValue", resp.Output())
	}
	if got := typed.Text(); got != "hello" {
		t.Fatalf("text = %q, want %q", got, "hello")
	}
	items := typed.Items()
	if len(items) != 2 || items[1].Kind != compatibility.ItemKindToolUse {
		t.Fatalf("items = %#v, want text + tool_use", items)
	}
}

func TestAnthropicAdapter_DecodesMessagesStreamingEvents(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Join([]string{
			`event: message_start`,
			`data: {"type":"message_start","message":{"id":"msg_1","model":"claude-3-7-sonnet"}}`,
			``,
			`event: content_block_start`,
			`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"he"}}`,
			``,
			`event: content_block_delta`,
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"llo"}}`,
			``,
			`event: content_block_stop`,
			`data: {"type":"content_block_stop","index":0}`,
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

	executor := anthropicadapter.NewExecutor(upstream.Client(), staticResolver("token-123"))
	resp, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewDialogRequest("claude-3-7-sonnet", []compatibility.CanonicalItem{
			compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
		}), ports.NewExecutionContract(compatibility.DeliveryModeStreaming),
		ports.NewRoutableTarget("backend-a", "anthropic", upstream.URL+"/v1", "cred-1", protocolsurface.Messages, "", ""),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	defer func() { _ = resp.Close() }()

	gotText := ""
	sawStarted := false
	sawCompleted := false
	for {
		event, err := resp.Stream().Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("stream next returned error: %v", err)
		}
		switch event.Kind {
		case compatibility.OutputEventStarted:
			sawStarted = true
		case compatibility.OutputEventTextDelta:
			gotText += event.TextDelta
		case compatibility.OutputEventCompleted:
			sawCompleted = true
		}
	}
	if !sawStarted || !sawCompleted {
		t.Fatalf("sawStarted=%v sawCompleted=%v, want both true", sawStarted, sawCompleted)
	}
	if gotText != "hello" {
		t.Fatalf("text delta = %q, want %q", gotText, "hello")
	}
}

type staticResolver string

func (r staticResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return string(r), nil
}
