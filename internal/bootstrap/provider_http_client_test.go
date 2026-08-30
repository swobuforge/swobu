package bootstrap

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/platform/outboundhttp"
)

func TestNewProviderHTTPClient_UsesOutboundHTTPTransport(t *testing.T) {
	client := newProviderHTTPClient()
	if client == nil {
		t.Fatal("newProviderHTTPClient returned nil")
	}
	if _, ok := client.Transport.(*outboundhttp.Transport); !ok {
		t.Fatalf("transport type = %T, want *outboundhttp.Transport", client.Transport)
	}
}

func TestNewProviderHTTPClient_DoesNotImposeAbsoluteInferenceHeaderDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := newProviderHTTPClient()
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("progressing inference before first response header failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
}

func TestNewProviderHTTPClient_CallerCancellationTerminatesRequestBeforeHeaders(t *testing.T) {
	requestStarted := make(chan struct{})
	requestCanceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(requestStarted)
		<-request.Context().Done()
		close(requestCanceled)
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("construct request: %v", err)
	}
	result := make(chan error, 1)
	go func() {
		response, requestErr := newProviderHTTPClient().Do(request)
		if response != nil {
			_ = response.Body.Close()
		}
		result <- requestErr
	}()

	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("provider request did not start")
	}
	cancel()

	select {
	case requestErr := <-result:
		if !errors.Is(requestErr, context.Canceled) {
			t.Fatalf("request error = %v, want context.Canceled", requestErr)
		}
	case <-time.After(time.Second):
		t.Fatal("provider request did not terminate after caller cancellation")
	}
	select {
	case <-requestCanceled:
	case <-time.After(time.Second):
		t.Fatal("provider handler did not observe caller cancellation")
	}
}

func TestNewDaemonHTTPServer_SetsTransportTimeouts(t *testing.T) {
	oldReadHeaderTimeout := daemonReadHeaderTimeout
	oldReadTimeout := daemonReadTimeout
	oldIdleTimeout := daemonIdleTimeout
	daemonReadHeaderTimeout = 3 * time.Second
	daemonReadTimeout = 4 * time.Second
	daemonIdleTimeout = 6 * time.Second
	t.Cleanup(func() {
		daemonReadHeaderTimeout = oldReadHeaderTimeout
		daemonReadTimeout = oldReadTimeout
		daemonIdleTimeout = oldIdleTimeout
	})

	server := newDaemonHTTPServer("127.0.0.1:7926", http.NewServeMux())
	if server == nil {
		t.Fatal("newDaemonHTTPServer returned nil")
	}
	if got := server.ReadHeaderTimeout; got != 3*time.Second {
		t.Fatalf("ReadHeaderTimeout = %v, want %v", got, 3*time.Second)
	}
	if got := server.ReadTimeout; got != 4*time.Second {
		t.Fatalf("ReadTimeout = %v, want %v", got, 4*time.Second)
	}
	if got := server.WriteTimeout; got != 0 {
		t.Fatalf("WriteTimeout = %v, want no absolute exchange deadline", got)
	}
	if got := server.IdleTimeout; got != 6*time.Second {
		t.Fatalf("IdleTimeout = %v, want %v", got, 6*time.Second)
	}
}
