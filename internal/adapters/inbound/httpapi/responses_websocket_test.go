package httpapi

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/websocket"

	"github.com/swobuforge/swobu/internal/app/requestpath"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/ports"
)

func TestResponsesWebsocket_AcceptsArbitraryOrigin(t *testing.T) {
	handler := NewHandler(staticRequestHandler{
		out: requestpath.HandleOutput{
			Response: ports.NewBufferedProviderResponse(canonical.NewConversationOutput(
				"chatcmpl_1",
				"model",
				[]canonical.OutputItem{canonical.NewTextOutputItem("text_0", "ok")},
				"stop",
			)),
		},
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/c/alpha/responses"
	conn, err := websocket.Dial(wsURL, "", "http://evil.example")
	if err != nil {
		t.Fatalf("websocket dial failed: %v", err)
	}
	defer func() { _ = conn.Close() }()
}

func TestResponsesWebsocket_AcceptsLocalOrigin(t *testing.T) {
	handler := NewHandler(staticRequestHandler{
		out: requestpath.HandleOutput{
			Response: ports.NewBufferedProviderResponse(canonical.NewConversationOutput(
				"chatcmpl_1",
				"model",
				[]canonical.OutputItem{canonical.NewTextOutputItem("text_0", "ok")},
				"stop",
			)),
		},
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/c/alpha/responses"
	conn, err := websocket.Dial(wsURL, "", "http://localhost")
	if err != nil {
		t.Fatalf("websocket dial failed: %v", err)
	}
	defer func() {
		_ = conn.Close()
	}()
}

func TestResponsesWebsocket_RejectsOversizedPayload(t *testing.T) {
	handler := NewHandler(staticRequestHandler{
		out: requestpath.HandleOutput{
			Response: ports.NewBufferedProviderResponse(canonical.NewConversationOutput(
				"chatcmpl_1",
				"model",
				[]canonical.OutputItem{canonical.NewTextOutputItem("text_0", "ok")},
				"stop",
			)),
		},
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/c/alpha/responses"
	conn, err := websocket.Dial(wsURL, "", "http://localhost")
	if err != nil {
		t.Fatalf("websocket dial failed: %v", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	huge := bytes.Repeat([]byte("a"), maxWebsocketRequestBodyBytes+1)
	payload := `{"type":"response.create","model":"m","input":"` + string(huge) + `"}`
	if err := websocket.Message.Send(conn, payload); err != nil {
		// Either transport-level frame limit or app-level BAD_REQUEST is acceptable.
		return
	}
	var msg string
	if err := websocket.Message.Receive(conn, &msg); err != nil {
		return
	}
	if !strings.Contains(msg, "BAD_REQUEST") {
		t.Fatalf("message = %q, want BAD_REQUEST", msg)
	}
}
