package httpapi

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/thread"
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
	mu        sync.Mutex
	ids       []string
	threadIDs []thread.ID
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
	i.threadIDs = append(i.threadIDs, in.ThreadID)
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
	config.Header.Set(openCodeSessionHeader, "secret-marker-123")
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
	wantThreadID, err := thread.Derive("client/x-opencode-session/v1", "alpha", "secret-marker-123")
	if err != nil {
		t.Fatal(err)
	}
	if len(ingress.threadIDs) != 2 || ingress.threadIDs[0] != wantThreadID || ingress.threadIDs[1] != wantThreadID {
		t.Fatal("websocket connection did not reuse one derived thread identity")
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

func TestResponsesWebsocket_RejectsCrossOriginBeforeIngress(t *testing.T) {
	ingress := &recordingWebsocketIngress{}
	handler := newTestHandler(ingress)
	server := httptest.NewServer(handler)
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/c/alpha/responses", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	request.Header.Set("Sec-WebSocket-Version", "13")
	request.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	request.Header.Set("Origin", "http://evil.example")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusForbidden)
	}
	ingress.mu.Lock()
	defer ingress.mu.Unlock()
	if len(ingress.ids) != 0 {
		t.Fatalf("exchange calls = %d, want zero", len(ingress.ids))
	}
}

func TestResponsesWebsocket_AcceptsExactLoopbackOrigin(t *testing.T) {
	handler := newTestHandler(staticRequestIngress{
		envelope: testProviderIngressFromOutput(canonicaltest.Response(t,
			"chatcmpl_1",
			"model",
			[]canonical.CanonicalItem{canonicaltest.MustMessage(canonical.MessageRoleAssistant, "ok")},
			canonical.Completed("stop"),
		)),
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/c/alpha/responses"
	conn, err := websocket.Dial(wsURL, "", server.URL)
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
			canonical.Completed("stop"),
		)),
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/c/alpha/responses"
	conn, err := websocket.Dial(wsURL, "", server.URL)
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

func TestValidateResponsesWebsocketAccess(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		host       string
		origins    []string
		tls        bool
		wantOK     bool
	}{
		{name: "native without origin", remoteAddr: "127.0.0.1:5000", host: "127.0.0.1:7926", wantOK: true},
		{name: "localhost exact", remoteAddr: "127.0.0.1:5000", host: "localhost:7926", origins: []string{"http://localhost:7926"}, wantOK: true},
		{name: "ipv4 exact", remoteAddr: "127.0.0.1:5000", host: "127.0.0.1:7926", origins: []string{"http://127.0.0.1:7926"}, wantOK: true},
		{name: "ipv6 exact", remoteAddr: "[::1]:5000", host: "[::1]:7926", origins: []string{"http://[::1]:7926"}, wantOK: true},
		{name: "default secure port", remoteAddr: "[::1]:5000", host: "[::1]", origins: []string{"https://[::1]:443"}, tls: true, wantOK: true},
		{name: "remote peer", remoteAddr: "192.0.2.4:5000", host: "127.0.0.1:7926"},
		{name: "missing peer", host: "127.0.0.1:7926"},
		{name: "remote origin", remoteAddr: "127.0.0.1:5000", host: "127.0.0.1:7926", origins: []string{"http://evil.example"}},
		{name: "null origin", remoteAddr: "127.0.0.1:5000", host: "127.0.0.1:7926", origins: []string{"null"}},
		{name: "different port", remoteAddr: "127.0.0.1:5000", host: "localhost:7926", origins: []string{"http://localhost:7927"}},
		{name: "localhost versus ipv4", remoteAddr: "127.0.0.1:5000", host: "127.0.0.1:7926", origins: []string{"http://localhost:7926"}},
		{name: "non loopback host", remoteAddr: "127.0.0.1:5000", host: "192.0.2.10:7926", origins: []string{"http://192.0.2.10:7926"}},
		{name: "rebound hostname", remoteAddr: "127.0.0.1:5000", host: "attacker.example:7926", origins: []string{"http://attacker.example:7926"}},
		{name: "origin path", remoteAddr: "127.0.0.1:5000", host: "127.0.0.1:7926", origins: []string{"http://127.0.0.1:7926/path"}},
		{name: "origin query", remoteAddr: "127.0.0.1:5000", host: "127.0.0.1:7926", origins: []string{"http://127.0.0.1:7926?x=1"}},
		{name: "wrong secure scheme", remoteAddr: "127.0.0.1:5000", host: "127.0.0.1:443", origins: []string{"http://127.0.0.1:443"}, tls: true},
		{name: "multiple origins", remoteAddr: "127.0.0.1:5000", host: "127.0.0.1:7926", origins: []string{"http://127.0.0.1:7926", "http://127.0.0.1:7926"}},
		{name: "malformed origin", remoteAddr: "127.0.0.1:5000", host: "127.0.0.1:7926", origins: []string{"://"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/c/alpha/responses", nil)
			request.RemoteAddr = test.remoteAddr
			request.Host = test.host
			request.Header.Del("Origin")
			for _, origin := range test.origins {
				request.Header.Add("Origin", origin)
			}
			if test.tls {
				request.TLS = &tls.ConnectionState{}
			}
			err := validateResponsesWebsocketAccess(request)
			if test.wantOK && err != nil {
				t.Fatalf("validateResponsesWebsocketAccess() error = %v", err)
			}
			if !test.wantOK && err == nil {
				t.Fatal("validateResponsesWebsocketAccess() succeeded, want rejection")
			}
		})
	}
}
