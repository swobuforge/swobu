package bootstrap

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestNewProviderHTTPClient_AppliesResponseHeaderTimeoutBehaviorally(t *testing.T) {
	old := providerResponseHeaderTimeout
	providerResponseHeaderTimeout = 50 * time.Millisecond
	t.Cleanup(func() { providerResponseHeaderTimeout = old })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client := newProviderHTTPClient()
	_, err := client.Get(server.URL)
	if err == nil {
		t.Fatal("expected a response-header timeout error, got nil")
	}
	var urlErr *url.Error
	if !errors.As(err, &urlErr) || !urlErr.Timeout() {
		t.Fatalf("error is not a timeout: %v", err)
	}
}

func TestNewDaemonHTTPServer_SetsTransportTimeouts(t *testing.T) {
	oldReadHeaderTimeout := daemonReadHeaderTimeout
	oldReadTimeout := daemonReadTimeout
	oldWriteTimeout := daemonWriteTimeout
	oldIdleTimeout := daemonIdleTimeout
	daemonReadHeaderTimeout = 3 * time.Second
	daemonReadTimeout = 4 * time.Second
	daemonWriteTimeout = 5 * time.Second
	daemonIdleTimeout = 6 * time.Second
	t.Cleanup(func() {
		daemonReadHeaderTimeout = oldReadHeaderTimeout
		daemonReadTimeout = oldReadTimeout
		daemonWriteTimeout = oldWriteTimeout
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
	if got := server.WriteTimeout; got != 5*time.Second {
		t.Fatalf("WriteTimeout = %v, want %v", got, 5*time.Second)
	}
	if got := server.IdleTimeout; got != 6*time.Second {
		t.Fatalf("IdleTimeout = %v, want %v", got, 6*time.Second)
	}
}
