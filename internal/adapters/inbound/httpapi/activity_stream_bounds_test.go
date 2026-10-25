package httpapi

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	evidence "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	"github.com/swobuforge/swobu/internal/ledger"
)

type activitySmallWriteListener struct{ net.Listener }

func (listener activitySmallWriteListener) Accept() (net.Conn, error) {
	connection, err := listener.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if tcp, ok := connection.(*net.TCPConn); ok {
		if err := tcp.SetWriteBuffer(1024); err != nil {
			connection.Close()
			return nil, err
		}
	}
	return connection, nil
}

func TestActivityStreamSlowSocketReleasesSessionAtWriteDeadline(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	returned := make(chan time.Time, 2)
	handler := NewActivityStreamHandler(func(request *http.Request, scope evidence.ProjectionScope, _ ledger.ActivityPageQuery) (evidence.ActivityProjection, error) {
		calls.Add(1)
		return evidence.ActivityProjection{Scope: scope, Exchanges: []evidence.ActivityExchange{{ExchangeID: strings.Repeat("x", 900<<10)}}}, nil
	})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		handler.ServeHTTP(writer, request)
		returned <- time.Now()
	}))
	server.Listener = activitySmallWriteListener{server.Listener}
	server.Start()
	defer server.Close()
	connection, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if tcp, ok := connection.(*net.TCPConn); ok {
		if err := tcp.SetReadBuffer(1024); err != nil {
			t.Fatal(err)
		}
	}
	started := time.Now()
	if _, err := fmt.Fprintf(connection, "GET /api/v1/activity/stream HTTP/1.1\r\nHost: %s\r\n\r\n", server.Listener.Addr()); err != nil {
		t.Fatal(err)
	}
	select {
	case stopped := <-returned:
		elapsed := stopped.Sub(started)
		if elapsed < 4*time.Second || elapsed > 9*time.Second || calls.Load() != 1 || len(handler.sessions) != 0 {
			t.Fatalf("slow socket lifetime=%s reads=%d sessions=%d", elapsed, calls.Load(), len(handler.sessions))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("slow socket exceeded actual write deadline")
	}
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), nil)
	if err != nil {
		t.Fatal("slow socket never received initial HTTP acknowledgement", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatal("fixture failed before SSE write")
	}
}

func TestActivityStreamActualMinuteLifetimeEndsWithoutClientCancellation(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	returned := make(chan time.Time, 1)
	handler := NewActivityStreamHandler(func(request *http.Request, scope evidence.ProjectionScope, _ ledger.ActivityPageQuery) (evidence.ActivityProjection, error) {
		calls.Add(1)
		return evidence.ActivityProjection{Scope: scope, Exchanges: []evidence.ActivityExchange{}}, nil
	})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		handler.ServeHTTP(writer, request)
		returned <- time.Now()
	}))
	defer server.Close()
	client := &http.Client{Timeout: 70 * time.Second}
	started := time.Now()
	response, err := client.Get(server.URL + "/api/v1/activity/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatal("lifetime fixture did not start")
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatal("stream did not end with ordinary EOF", err)
	}
	stopped := <-returned
	elapsed := stopped.Sub(started)
	if elapsed < 59*time.Second || elapsed > 65*time.Second || calls.Load() < 50 || calls.Load() > 61 || len(handler.sessions) != 0 {
		t.Fatalf("minute lifetime=%s reads=%d sessions=%d", elapsed, calls.Load(), len(handler.sessions))
	}
	count := calls.Load()
	time.Sleep(1100 * time.Millisecond)
	if calls.Load() != count {
		t.Fatal("expired stream continued reading evidence")
	}
}
