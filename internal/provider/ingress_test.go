package provider

import (
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
)

func TestValidateIngressRequiresByteStreamBody(t *testing.T) {
	if err := ValidateIngress(StreamIngress{Stream: carrier.ByteStream{MediaType: "text/event-stream"}}); err == nil {
		t.Fatal("nil byte-stream body must be rejected")
	}
	if err := ValidateIngress(StreamIngress{Stream: carrier.ByteStream{MediaType: "text/event-stream",
		Body: io.NopCloser(strings.NewReader("data")),
	}}); err != nil {
		t.Fatalf("non-nil byte-stream body rejected: %v", err)
	}
}
