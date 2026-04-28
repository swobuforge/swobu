package httpapi_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/websocket"

	"github.com/metrofun/swobu/internal/adapters/inbound/httpapi"
	"github.com/metrofun/swobu/internal/app/operator/clientprofile"
	"github.com/metrofun/swobu/internal/app/requestpath"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/ports"
)

func TestClientFlow_CodexResponsesWebsocketReconnectContract(t *testing.T) {
	t.Parallel()

	handler := httpapi.NewHandler(clientFlowStaticRequestHandler{
		out: requestpath.HandleOutput{
			Response: ports.NewStreamingExecuteResponse(compatibility.NewSliceEventStream([]compatibility.OutputEvent{
				{Kind: compatibility.OutputEventStarted, ResultID: "resp_1", Model: "m"},
				{Kind: compatibility.OutputEventTextDelta, ItemID: "text_0", TextDelta: "ok"},
				{Kind: compatibility.OutputEventCompleted, FinishReason: "completed"},
			})),
		},
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/c/acme/responses"
	cfg, err := websocket.NewConfig(wsURL, server.URL)
	if err != nil {
		t.Fatalf("NewConfig returned error: %v", err)
	}
	cfg.Header.Set("User-Agent", "Codex/0.122.0")
	conn, err := websocket.DialConfig(cfg)
	if err != nil {
		t.Fatalf("DialConfig returned error: %v", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	if err := websocket.Message.Send(conn, `{"type":"response.create","model":"m","input":"hi","stream":true}`); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	var joined strings.Builder
	for {
		var frame string
		if err := websocket.Message.Receive(conn, &frame); err != nil {
			t.Fatalf("Receive returned error: %v", err)
		}
		joined.WriteString(frame)
		if strings.Contains(frame, `"type":"response.completed"`) {
			break
		}
	}

	all := joined.String()
	if !strings.Contains(all, `"type":"response.created"`) {
		t.Fatalf("frames = %q, want response.created", all)
	}
	if !strings.Contains(all, `"type":"response.output_item.added"`) {
		t.Fatalf("frames = %q, want response.output_item.added", all)
	}
	if !strings.Contains(all, `"type":"response.content_part.added"`) {
		t.Fatalf("frames = %q, want response.content_part.added", all)
	}
	if !strings.Contains(all, `"type":"response.output_text.delta"`) {
		t.Fatalf("frames = %q, want response.output_text.delta", all)
	}
	if !strings.Contains(all, `"type":"response.completed"`) {
		t.Fatalf("frames = %q, want response.completed", all)
	}
	if strings.Contains(all, `"type":"error"`) {
		t.Fatalf("frames = %q, want no error frames", all)
	}
	if strings.Index(all, `"type":"response.output_item.added"`) > strings.Index(all, `"type":"response.output_text.delta"`) {
		t.Fatalf("frames = %q, want output_item.added before output_text.delta", all)
	}
}

func TestClientFlow_ContinueApiBaseExecutesRealChatPath(t *testing.T) {
	t.Parallel()

	handler := httpapi.NewHandler(clientFlowStaticRequestHandler{
		out: requestpath.HandleOutput{
			Response: ports.NewBufferedExecuteResponse(compatibility.NewConversationOutput(
				"chatcmpl_1",
				"m",
				[]compatibility.OutputItem{compatibility.NewTextOutputItem("text_0", "ok")},
				"stop",
			)),
		},
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	baseURL := server.URL + "/c/acme/"
	profile := clientprofile.FindByID(clientprofile.Catalog(), "continue")
	if profile == nil {
		t.Fatal("missing continue profile")
	}
	actions := profile.Actions(baseURL)
	if len(actions) == 0 {
		t.Fatal("continue profile actions missing")
	}
	snippet := actions[0].Content
	if !strings.Contains(snippet, "apiBase: "+server.URL+"/c/acme/v1") {
		t.Fatalf("continue snippet apiBase mismatch: %q", snippet)
	}

	reqBody := `{"model":"primary","messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(server.URL+"/c/acme/v1/chat/completions", "application/json", bytes.NewBufferString(reqBody))
	if err != nil {
		t.Fatalf("POST returned error: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

type clientFlowStaticRequestHandler struct {
	out requestpath.HandleOutput
	err error
}

func (h clientFlowStaticRequestHandler) Handle(_ context.Context, _ requestpath.HandleInput) (requestpath.HandleOutput, error) {
	return h.out, h.err
}
