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
