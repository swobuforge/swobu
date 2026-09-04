package httpapi

import (
	"net/http"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/thread"
)

func TestIngressTransportRequestConsumesFirstNonblankOpenCodeSession(t *testing.T) {
	header := http.Header{
		openCodeSessionHeader: []string{"  ", "secret-marker-123", "ignored-marker"},
		"X-Request-Marker":    []string{"preserved"},
	}

	request, got, err := ingressTransportRequest(http.MethodPost, "/responses", "alpha", header, []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	want, err := thread.Derive("client/x-opencode-session/v1", "alpha", "secret-marker-123")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatal("ingress did not derive identity from the first nonblank header value")
	}
	if request.Header.Get(openCodeSessionHeader) != "" {
		t.Fatal("raw OpenCode session header survived ingress sanitization")
	}
	if request.Header.Get("X-Request-Marker") != "preserved" {
		t.Fatal("ingress sanitization removed an unrelated header")
	}
	if header.Values(openCodeSessionHeader)[1] != "secret-marker-123" {
		t.Fatal("ingress mutated the caller-owned header map")
	}
}

func TestIngressTransportRequestLeavesBlankOpenCodeSessionUnknown(t *testing.T) {
	request, got, err := ingressTransportRequest(http.MethodPost, "/responses", "alpha", http.Header{
		openCodeSessionHeader: []string{"", " \t "},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsZero() {
		t.Fatal("blank-only OpenCode session header derived a thread identity")
	}
	if request.Header.Get(openCodeSessionHeader) != "" {
		t.Fatal("blank OpenCode session header survived ingress sanitization")
	}
}
