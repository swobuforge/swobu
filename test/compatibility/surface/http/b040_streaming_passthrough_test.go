package http_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	httpapi "github.com/metrofun/swobu/internal/adapters/inbound/httpapi"
	"github.com/metrofun/swobu/internal/app/requestpath"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/ports"
)

func TestB040_StreamingPassthrough(t *testing.T) {
	canceled := make(chan struct{})
	stream := &cancelAwareStream{
		events: []compatibility.OutputEvent{
			{Kind: compatibility.OutputEventStarted, ResultID: "chatcmpl_1", Model: "m"},
			{Kind: compatibility.OutputEventTextDelta, TextDelta: "ok"},
		},
		canceled: canceled,
	}
	handler := httpapi.NewHandler(streamingRequestHandler{
		out: requestpath.HandleOutput{
			Response: ports.NewStreamingExecuteResponse(stream),
		},
		onRequest: func(ctx context.Context) {
			stream.ctx = ctx
		},
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/c/alpha/chat/completions", bytes.NewBufferString(`{"model":"m","messages":[{"role":"user","content":"hi"}],"stream":true}`))
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do returned error: %v", err)
	}
	buf := make([]byte, 512)
	n, err := resp.Body.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("Read returned error: %v", err)
	}
	if n == 0 {
		t.Fatal("expected first streaming bytes, got none")
	}
	if !bytes.Contains(buf[:n], []byte("data:")) {
		t.Fatalf("first chunk = %q, want SSE data frame", string(buf[:n]))
	}
	_ = resp.Body.Close()

	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("client disconnect did not cancel backend request")
	}
}

type streamingRequestHandler struct {
	out       requestpath.HandleOutput
	onRequest func(context.Context)
}

func (h streamingRequestHandler) Handle(ctx context.Context, in requestpath.HandleInput) (requestpath.HandleOutput, error) {
	if h.onRequest != nil {
		h.onRequest(ctx)
	}
	return h.out, nil
}

type cancelAwareStream struct {
	ctx      context.Context
	events   []compatibility.OutputEvent
	index    int
	canceled chan struct{}
	once     sync.Once
}

func (s *cancelAwareStream) Next() (compatibility.OutputEvent, error) {
	if s.index < len(s.events) {
		event := s.events[s.index]
		s.index++
		return event, nil
	}
	<-s.ctx.Done()
	s.once.Do(func() { close(s.canceled) })
	return compatibility.OutputEvent{}, io.EOF
}

func (s *cancelAwareStream) Close() error {
	s.once.Do(func() { close(s.canceled) })
	return nil
}
