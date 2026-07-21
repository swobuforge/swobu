package httpapi

import (
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

type recordingWebsocketFrameWriter struct{ frames [][]byte }

func (w *recordingWebsocketFrameWriter) WriteFrame(frame []byte) error {
	w.frames = append(w.frames, append([]byte(nil), frame...))
	return nil
}

type oneWebsocketMessage struct {
	message []byte
	done    bool
}

func (s *oneWebsocketMessage) Next(context.Context) ([]byte, error) {
	if s.done {
		return nil, io.EOF
	}
	s.done = true
	return append([]byte(nil), s.message...), nil
}

func (*oneWebsocketMessage) Close(context.Context) error { return nil }

func TestDrainWebsocketMessagesPreservesOneLargeMessageBoundary(t *testing.T) {
	want := bytes.Repeat([]byte("x"), 5000)
	sink := &recordingWebsocketFrameWriter{}
	_, err := drainWebsocketMessagesWithStats(context.Background(), &oneWebsocketMessage{message: want}, sink)
	if err != nil {
		t.Fatal(err)
	}
	if len(sink.frames) != 1 {
		t.Fatalf("websocket messages = %d, want 1", len(sink.frames))
	}
	if !bytes.Equal(sink.frames[0], want) {
		t.Fatalf("message bytes = %d, want %d", len(sink.frames[0]), len(want))
	}
}

type recordingWebsocketIngress struct {
	mu  sync.Mutex
	ids []string
}

type cancellationWebsocketIngress struct {
	started  chan struct{}
	canceled chan struct{}
}

type serialWebsocketIngress struct {
	mu        sync.Mutex
	active    int
	maxActive int
}

func (i *serialWebsocketIngress) HandleRequest(ctx context.Context, in exchange.RequestInput) (exchange.RequestOutput, error) {
	i.mu.Lock()
	i.active++
	if i.active > i.maxActive {
		i.maxActive = i.active
	}
	i.mu.Unlock()
	time.Sleep(25 * time.Millisecond)
	out, err := synthesizeRequestOutputFromEnvelope(ctx, in, testStreamingTextResponse("resp", "m", "text", "ok", "completed"))
	i.mu.Lock()
	i.active--
	i.mu.Unlock()
	return out, err
}

func (i cancellationWebsocketIngress) HandleRequest(ctx context.Context, _ exchange.RequestInput) (exchange.RequestOutput, error) {
	close(i.started)
	<-ctx.Done()
	close(i.canceled)
	return exchange.RequestOutput{}, ctx.Err()
}

func (i *recordingWebsocketIngress) HandleRequest(ctx context.Context, in exchange.RequestInput) (exchange.RequestOutput, error) {
	i.mu.Lock()
	i.ids = append(i.ids, in.ExchangeID)
	i.mu.Unlock()
	return synthesizeRequestOutputFromEnvelope(ctx, in, testStreamingTextResponse("resp", "m", "text", "ok", "completed"))
}

func TestResponsesWebsocket_AllocatesDistinctExchangeIdentityPerCreate(t *testing.T) {
	ingress := &recordingWebsocketIngress{}
	server := httptest.NewServer(newTestHandler(ingress))
	defer server.Close()
	config, err := websocket.NewConfig("ws"+strings.TrimPrefix(server.URL, "http")+"/c/alpha/responses", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	config.Header.Set("X-Request-ID", "connection-id")
	conn, err := websocket.DialConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	for range 2 {
		if err := websocket.Message.Send(conn, `{"type":"response.create","model":"m","input":"hi","stream":true}`); err != nil {
			t.Fatal(err)
		}
		for {
			var message string
			if err := websocket.Message.Receive(conn, &message); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(message, `"type":"response.completed"`) {
				break
			}
		}
	}
	ingress.mu.Lock()
	defer ingress.mu.Unlock()
	if len(ingress.ids) != 2 || ingress.ids[0] == ingress.ids[1] {
		t.Fatalf("exchange ids = %#v, want two distinct identities", ingress.ids)
	}
}

func TestResponsesWebsocket_DisconnectCancelsActiveExchange(t *testing.T) {
	ingress := cancellationWebsocketIngress{started: make(chan struct{}), canceled: make(chan struct{})}
	server := httptest.NewServer(newTestHandler(ingress))
	defer server.CloseClientConnections()
	conn, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/c/alpha/responses", "", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := websocket.Message.Send(conn, `{"type":"response.create","model":"m","input":"hi","stream":true}`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ingress.started:
	case <-time.After(time.Second):
		t.Fatal("exchange did not start")
	}
	_ = conn.Close()
	select {
	case <-ingress.canceled:
	case <-time.After(time.Second):
		t.Fatal("websocket disconnect did not cancel active exchange")
	}
}

func TestResponsesWebsocket_ProcessesQueuedCreatesSerially(t *testing.T) {
	ingress := &serialWebsocketIngress{}
	server := httptest.NewServer(newTestHandler(ingress))
	defer server.CloseClientConnections()
	conn, err := websocket.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/c/alpha/responses", "", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	request := `{"type":"response.create","model":"m","input":"hi","stream":true}`
	if err := websocket.Message.Send(conn, request); err != nil {
		t.Fatal(err)
	}
	if err := websocket.Message.Send(conn, request); err != nil {
		t.Fatal(err)
	}
	completed := 0
	for completed < 2 {
		var message string
		if err := websocket.Message.Receive(conn, &message); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(message, `"type":"response.completed"`) {
			completed++
		}
	}
	ingress.mu.Lock()
	defer ingress.mu.Unlock()
	if ingress.maxActive != 1 {
		t.Fatalf("maximum concurrent exchanges = %d, want serial ownership", ingress.maxActive)
	}
}

func TestResponsesWebsocket_AcceptsArbitraryOrigin(t *testing.T) {
	handler := newTestHandler(staticRequestIngress{
		envelope: testProviderIngressFromOutput(canonicaltest.Response(t,
			"chatcmpl_1",
			"model",
			[]canonical.CanonicalItem{canonicaltest.MustMessage(canonical.MessageRoleAssistant, "ok")},
			"stop",
		)),
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
	handler := newTestHandler(staticRequestIngress{
		envelope: testProviderIngressFromOutput(canonicaltest.Response(t,
			"chatcmpl_1",
			"model",
			[]canonical.CanonicalItem{canonicaltest.MustMessage(canonical.MessageRoleAssistant, "ok")},
			"stop",
		)),
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
	handler := newTestHandler(staticRequestIngress{
		envelope: testProviderIngressFromOutput(canonicaltest.Response(t,
			"chatcmpl_1",
			"model",
			[]canonical.CanonicalItem{canonicaltest.MustMessage(canonical.MessageRoleAssistant, "ok")},
			"stop",
		)),
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

	huge := bytes.Repeat([]byte("a"), int(maxOperatorJSONBodyBytes)+1)
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
