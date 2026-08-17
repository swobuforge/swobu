package exchange

import (
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestBuildTerminalTrafficEventPreservesResolvedProviderAndModel(t *testing.T) {
	routeName, _ := routing.ParseRouteName("openai")
	target := provider.NewTargetSnapshot("tgt_opaque", "anthropic", "https://api.anthropic.com", "env:KEY", protocolkind.Messages, "messages", delivery.BufferedDelivery())
	target.Model = "claude-sonnet-4-6"
	event, err := BuildTerminalTrafficEvent(&TrafficEvidenceInput{
		workspace:   requestpathWorkspace(t),
		routeName:   routeName,
		exchangeID:  "req_target_evidence",
		requestPath: canonical.NormalizedPathResponses,
		request:     canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("openai")}),
		target:      target,
	}, delivery.Result{Kind: delivery.Succeeded}, trafficevidence.NewUnknownTiming())
	if err != nil {
		t.Fatal(err)
	}
	if event.ProviderSpec() != "anthropic" || event.ProviderModel() != "claude-sonnet-4-6" {
		t.Fatalf("resolved target evidence = %q/%q", event.ProviderSpec(), event.ProviderModel())
	}
}

func TestBuildTerminalTrafficEventExposesPossibleDuplicateProviderWork(t *testing.T) {
	routeName, _ := routing.ParseRouteName("openai")
	target := provider.NewTargetSnapshot("target-a", "openai", "https://api.openai.com", "env:KEY", protocolkind.Responses, "responses", delivery.BufferedDelivery())
	target.Model = "model"
	event, err := BuildTerminalTrafficEvent(&TrafficEvidenceInput{
		workspace:   requestpathWorkspace(t),
		routeName:   routeName,
		exchangeID:  "req_duplicate_risk",
		requestPath: canonical.NormalizedPathResponses,
		request:     canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("openai")}),
		target:      target,
		routing: terminalRoutingEvidence{
			providerCallCount:          2,
			possibleDuplicateExecution: true,
		},
	}, delivery.Result{Kind: delivery.Succeeded}, trafficevidence.NewUnknownTiming())
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := event.ExchangeDiagnostics()
	if len(diagnostics) != 1 || diagnostics[0] != "possible_duplicate_provider_execution_and_cost" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestBuildTerminalTrafficEventCarriesReusablePrefixEvidence(t *testing.T) {
	prefix, err := trafficevidence.NewChangedReusablePrefix(trafficevidence.ReusablePrefixTool)
	if err != nil {
		t.Fatal(err)
	}
	event, err := BuildTerminalTrafficEvent(&TrafficEvidenceInput{
		workspace: requestpathWorkspace(t), exchangeID: "req-prefix", requestPath: canonical.NormalizedPathResponses,
		request: testCanonicalRequest("model"), target: provider.TargetSnapshot{TargetID: "target", TargetVersion: 4, ProviderSpec: "openai", Model: "model", ProtocolKind: protocolkind.Responses},
		reusablePrefix: prefix,
	}, delivery.Result{Kind: delivery.Succeeded}, trafficevidence.NewUnknownTiming())
	if err != nil {
		t.Fatal(err)
	}
	if event.ReusablePrefix().State() != trafficevidence.ReusablePrefixChanged || event.TargetProtocol() != protocolkind.Responses || event.TargetVersion() != 4 {
		t.Fatalf("terminal cache evidence = %#v/%q/%d", event.ReusablePrefix(), event.TargetProtocol(), event.TargetVersion())
	}
}

func TestBuildTerminalTrafficEventCarriesCompletionCacheUsage(t *testing.T) {
	zero, positive := 0, 12
	usage, _ := canonical.NewTokenUsage(canonical.TokenUsageParams{CacheReadTokens: &zero, CacheWriteTokens: &positive})
	completion, complete, _ := wire.NewResponseCompletion()
	completion.ObserveUsage(usage)
	complete(nil, nil)
	event, err := BuildTerminalTrafficEvent(&TrafficEvidenceInput{
		workspace: requestpathWorkspace(t), exchangeID: "req-cache-usage", requestPath: canonical.NormalizedPathResponses,
		request: testCanonicalRequest("model"), target: provider.TargetSnapshot{TargetID: "target", TargetVersion: 1, ProviderSpec: "openai", Model: "model", ProtocolKind: protocolkind.Responses},
		response: BufferedResponse{completion: completion},
	}, delivery.Result{Kind: delivery.Succeeded}, trafficevidence.NewUnknownTiming())
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := event.TokenUsage().CacheReadTokens(); !ok || value != 0 {
		t.Fatalf("cache read = %d, %t", value, ok)
	}
	if value, ok := event.TokenUsage().CacheWriteTokens(); !ok || value != 12 {
		t.Fatalf("cache write = %d, %t", value, ok)
	}
}
