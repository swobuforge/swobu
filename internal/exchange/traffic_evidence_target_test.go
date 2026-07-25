package exchange

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
	transportpkg "github.com/swobuforge/swobu/internal/transport"
)

func TestBuildTerminalTrafficEventPreservesResolvedProviderAndModel(t *testing.T) {
	routeName, _ := routing.ParseRouteName("openai")
	target := provider.NewTargetSnapshot("tgt_opaque", "anthropic", "https://api.anthropic.com", "env:KEY", "messages", "", "messages")
	target.Model = "claude-sonnet-4-6"
	event, err := BuildTerminalTrafficEvent(&TrafficEvidenceInput{
		workspace:   requestpathWorkspace(t),
		routeName:   routeName,
		exchangeID:  "req_target_evidence",
		requestPath: canonical.NormalizedPathResponses,
		request:     canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("openai")}),
		target:      target,
	}, transportpkg.DeliveryResult{Kind: transportpkg.DeliverySucceeded}, trafficevidence.NewUnknownTiming())
	if err != nil {
		t.Fatal(err)
	}
	if event.ProviderSpec() != "anthropic" || event.ProviderModel() != "claude-sonnet-4-6" {
		t.Fatalf("resolved target evidence = %q/%q", event.ProviderSpec(), event.ProviderModel())
	}
}
