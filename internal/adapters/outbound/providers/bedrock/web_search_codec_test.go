package bedrock

import (
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestBedrockWebSearchFailsBeforeTransport(t *testing.T) {
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{canonical.NewWebSearchDeclaration()})
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model"), Tools: canonical.Specify(set)})
	_, _, err := (webSearchBackendCodec{standard: protocolcodec.Codec{Protocol: protocolkind.Responses}}).Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	var unsupported provider.UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("error = %T %v, want provider.UnsupportedError", err, err)
	}
}
