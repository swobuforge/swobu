package messages

import (
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestMessagesEagerlyMaterializesUnrepresentableProviderOwnedDiscovery(t *testing.T) {
	discovery, err := canonical.NewToolDiscoveryTool(
		"find a tool",
		canonicaltest.Schema(t, `{"type":"object"}`),
		canonical.DiscoveryExecutorProvider,
	)
	if err != nil {
		t.Fatal(err)
	}
	loaded := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "weather"), "weather", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, discovery, loaded),
			canonicaltest.Message(t, canonical.MessageRoleUser, "find an appropriate tool"),
		},
	})
	var changes []compat.Change
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	document, err := CompileProviderRequestDocument(request, names, delivery.BufferedDelivery(), &changes, "exchange", CompileOptions{Lowering: DefaultLowering()})
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Tools) != 1 || document.Tools[0].Name != "weather" || document.Tools[0].DeferLoading {
		t.Fatalf("tools=%#v, want eager weather only", document.Tools)
	}
	if len(changes) != 1 || changes[0].Capability != canonical.RequestToolsVisibility || changes[0].Kind != compat.Approximation {
		t.Fatalf("changes=%#v, want one visibility approximation", changes)
	}
}

func TestMessagesEagerProviderDiscoverySpecificPolicyRecordsOrdinaryPolicyLoss(t *testing.T) {
	discovery, err := canonical.NewToolDiscoveryTool("find a tool", canonicaltest.Schema(t, `{"type":"object"}`), canonical.DiscoveryExecutorProvider)
	if err != nil {
		t.Fatal(err)
	}
	loaded := canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "weather"), "weather", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	key := discovery.Key()
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:      canonical.Specify("model"),
		ToolPolicy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicySpecific, &key)),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, discovery, loaded),
			canonicaltest.Message(t, canonical.MessageRoleUser, "find an appropriate tool"),
		},
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	var changes []compat.Change
	document, err := CompileProviderRequestDocument(request, names, delivery.BufferedDelivery(), &changes, "exchange", CompileOptions{Lowering: DefaultLowering()})
	if err != nil {
		t.Fatal(err)
	}
	if document.Payload["tool_choice"] != nil {
		t.Fatalf("tool_choice=%#v, want omitted after discovery eager lowering", document.Payload["tool_choice"])
	}
	seenVisibility, seenPolicy := false, false
	for _, change := range changes {
		seenVisibility = seenVisibility || change.Capability == canonical.RequestToolsVisibility && change.Kind == compat.Approximation
		seenPolicy = seenPolicy || change.Capability == canonical.RequestToolPolicy && change.Kind == compat.Omission
	}
	if !seenVisibility || !seenPolicy {
		t.Fatalf("changes=%#v, want visibility approximation and policy omission", changes)
	}
}
