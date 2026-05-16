package runtime

import (
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/ports"
)

func TestSelectResponseDecoder(t *testing.T) {
	streaming := func(body io.ReadCloser) (ports.ProviderResponse, error) {
		_ = body.Close()
		return ports.ProviderResponse{}, nil
	}
	buffered := func(body io.ReadCloser) (ports.ProviderResponse, error) {
		_ = body.Close()
		return ports.ProviderResponse{}, nil
	}

	if dec, ok := SelectResponseDecoder(true, nil, buffered); ok || dec != nil {
		t.Fatalf("expected missing streaming decoder to fail selection")
	}
	if dec, ok := SelectResponseDecoder(false, streaming, nil); ok || dec != nil {
		t.Fatalf("expected missing buffered decoder to fail selection")
	}
	if dec, ok := SelectResponseDecoder(true, streaming, buffered); !ok || dec == nil {
		t.Fatalf("expected streaming decoder to be selected")
	}
	if dec, ok := SelectResponseDecoder(false, streaming, buffered); !ok || dec == nil {
		t.Fatalf("expected buffered decoder to be selected")
	}
}

func TestRequireProviderAndProtocol(t *testing.T) {
	if err := RequireProviderAndProtocol(
		string(providercatalog.ProviderSpecAnthropic),
		providercatalog.ProviderSpecAnthropic,
		protocolkind.Messages,
		protocolkind.Messages,
		"anthropic",
	); err != nil {
		t.Fatalf("expected valid provider/protocol to pass, got err=%v", err)
	}

	if err := RequireProviderAndProtocol(
		"not-a-provider",
		providercatalog.ProviderSpecAnthropic,
		protocolkind.Messages,
		protocolkind.Messages,
		"anthropic",
	); err == nil || !strings.Contains(err.Error(), "provider id") {
		t.Fatalf("expected provider-id validation error, got err=%v", err)
	}

	if err := RequireProviderAndProtocol(
		string(providercatalog.ProviderSpecAnthropic),
		providercatalog.ProviderSpecAnthropic,
		protocolkind.Responses,
		protocolkind.Messages,
		"anthropic",
	); err == nil || !strings.Contains(err.Error(), "requires") {
		t.Fatalf("expected protocol validation error, got err=%v", err)
	}
}
