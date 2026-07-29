package messages

import (
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestMessagesRejectsLiveDiscoveryCapabilityForRouteFallback(t *testing.T) {
	discovery, err := canonical.NewToolDiscoveryTool(
		"find a tool",
		canonicaltest.Schema(t, `{"type":"object"}`),
		canonical.DiscoveryExecutorProvider,
	)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, discovery),
			canonicaltest.Message(t, canonical.MessageRoleUser, "find an appropriate tool"),
		},
	})
	_, err = LowerProviderRequestDocument(request, delivery.BufferedDelivery(), nil, "exchange")
	var incompatible provider.IncompatibleTargetError
	if !errors.As(err, &incompatible) {
		t.Fatalf("error = %T %v, want target incompatibility", err, err)
	}
}
