package runtime

import (
	"github.com/swobuforge/swobu/internal/delivery"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/ports"
	"github.com/swobuforge/swobu/internal/profile"
)

func TestSelectResponseDecoder(t *testing.T) {
	streaming := func(stream carrier.WireStream) (ports.ProviderResponseStream, error) {
		_ = stream
		return ports.ProviderResponseStream{}, nil
	}
	buffered := func(stream carrier.WireStream) (ports.ProviderResponseStream, error) {
		_ = stream
		return ports.ProviderResponseStream{}, nil
	}

	if dec, ok := SelectResponseDecoder(delivery.Streaming, nil, buffered); ok || dec != nil {
		t.Fatalf("expected missing streaming decoder to fail selection")
	}
	if dec, ok := SelectResponseDecoder(delivery.Buffered, streaming, nil); ok || dec != nil {
		t.Fatalf("expected missing buffered decoder to fail selection")
	}
	if dec, ok := SelectResponseDecoder(delivery.Streaming, streaming, buffered); !ok || dec == nil {
		t.Fatalf("expected streaming decoder to be selected")
	}
	if dec, ok := SelectResponseDecoder(delivery.Buffered, streaming, buffered); !ok || dec == nil {
		t.Fatalf("expected buffered decoder to be selected")
	}
}

func TestRequireProviderAndProtocol(t *testing.T) {
	if err := RequireProviderAndProtocol(
		string(profile.ProviderSpecAnthropic),
		profile.ProviderSpecAnthropic,
		protocolkind.Messages,
		protocolkind.Messages,
		"anthropic",
	); err != nil {
		t.Fatalf("expected valid provider/protocol to pass, got err=%v", err)
	}

	if err := RequireProviderAndProtocol(
		"not-a-provider",
		profile.ProviderSpecAnthropic,
		protocolkind.Messages,
		protocolkind.Messages,
		"anthropic",
	); err == nil || !strings.Contains(err.Error(), "provider id") {
		t.Fatalf("expected provider-id validation error, got err=%v", err)
	}

	if err := RequireProviderAndProtocol(
		string(profile.ProviderSpecAnthropic),
		profile.ProviderSpecAnthropic,
		protocolkind.Responses,
		protocolkind.Messages,
		"anthropic",
	); err == nil || !strings.Contains(err.Error(), "requires") {
		t.Fatalf("expected protocol validation error, got err=%v", err)
	}
}
