package http_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/websocket"

	"github.com/metrofun/swobu/internal/adapters/inbound/httpapi"
	"github.com/metrofun/swobu/internal/app/requestpath"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/ports"
)

func TestB020_IngressTransportContract(t *testing.T) {
	t.Run("websocket upgrade on compatibility path is rejected explicitly", func(t *testing.T) {
		handler := httpapi.NewHandler(fakeRequestHandler{})
		req := httptest.NewRequest(http.MethodGet, "/c/alpha/chat/completions", nil)
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
		body := rec.Body.Bytes()
		if !bytes.Contains(body, []byte(`"code":"UNSUPPORTED_ENDPOINT"`)) {
			t.Fatalf("body = %q, want UNSUPPORTED_ENDPOINT", body)
		}
		if !bytes.Contains(body, []byte(`supported only on compatibility /responses routes`)) {
			t.Fatalf("body = %q, want /responses guidance", body)
		}
	})

	t.Run("codex-style websocket reconnect on responses is accepted", func(t *testing.T) {
		handler := httpapi.NewHandler(fakeRequestHandler{
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

		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/c/test/responses"
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
			var message string
			if err := websocket.Message.Receive(conn, &message); err != nil {
				t.Fatalf("Receive returned error: %v", err)
			}
			joined.WriteString(message)
			if strings.Contains(message, `"type":"response.completed"`) {
				break
			}
		}
		body := []byte(joined.String())
		if !bytes.Contains(body, []byte(`"type":"response.created"`)) {
			t.Fatalf("messages = %q, want response.created", body)
		}
		if !bytes.Contains(body, []byte(`"type":"response.output_item.added"`)) {
			t.Fatalf("messages = %q, want response.output_item.added", body)
		}
		if !bytes.Contains(body, []byte(`"type":"response.content_part.added"`)) {
			t.Fatalf("messages = %q, want response.content_part.added", body)
		}
		if !bytes.Contains(body, []byte(`"type":"response.output_text.delta"`)) {
			t.Fatalf("messages = %q, want response.output_text.delta", body)
		}
		if !bytes.Contains(body, []byte(`"type":"response.completed"`)) {
			t.Fatalf("messages = %q, want response.completed", body)
		}
		if bytes.Index(body, []byte(`"type":"response.output_item.added"`)) > bytes.Index(body, []byte(`"type":"response.output_text.delta"`)) {
			t.Fatalf("messages = %q, want output_item.added before output_text.delta", body)
		}
		if bytes.Contains(body, []byte(`"type":"error"`)) {
			t.Fatalf("messages = %q, want no error", body)
		}
	})

	t.Run("non-POST compatibility family operation is rejected with method guidance", func(t *testing.T) {
		handler := httpapi.NewHandler(fakeRequestHandler{})
		req := httptest.NewRequest(http.MethodGet, "/c/alpha/responses", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
		}
		body := rec.Body.Bytes()
		if !bytes.Contains(body, []byte(`"code":"UNSUPPORTED_ENDPOINT"`)) {
			t.Fatalf("body = %q, want UNSUPPORTED_ENDPOINT", body)
		}
		if !bytes.Contains(body, []byte(`compatibility family operations require HTTP POST`)) {
			t.Fatalf("body = %q, want POST guidance", body)
		}
	})
}
