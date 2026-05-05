package bootstrap

import (
	"net/http"
	"testing"
	"time"
)

func TestNewProviderHTTPClient_SetsResponseHeaderTimeout(t *testing.T) {
	old := providerResponseHeaderTimeout
	providerResponseHeaderTimeout = 2 * time.Second
	t.Cleanup(func() {
		providerResponseHeaderTimeout = old
	})

	client := newProviderHTTPClient()
	if client == nil {
		t.Fatal("newProviderHTTPClient returned nil")
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T, want *http.Transport", client.Transport)
	}
	if got := transport.ResponseHeaderTimeout; got != 2*time.Second {
		t.Fatalf("ResponseHeaderTimeout = %v, want %v", got, 2*time.Second)
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
