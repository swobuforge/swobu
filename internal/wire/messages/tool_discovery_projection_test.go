package messages

import (
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestMessagesOmitsUnsupportedLiveDiscoveryCapability(t *testing.T) {
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
	var changes []compat.Change
	document, err := CompileProviderRequestDocument(request, nil, delivery.BufferedDelivery(), &changes, "exchange", CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Tools) != 0 {
		t.Fatalf("unsupported discovery leaked into request: %#v", document.Tools)
	}
	want := compat.NewOmission(canonical.RequestToolsKind, canonical.ToolOccurrence(discovery.Key()))
	if len(changes) != 1 || changes[0] != want {
		t.Fatalf("changes = %#v, want %#v", changes, want)
	}
}
