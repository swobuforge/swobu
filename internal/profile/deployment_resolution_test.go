package profile

import (
	"reflect"
	"testing"
)

func TestResolveModelAuthoringOption_ExplicitFactsWinOverProviderDefaults(t *testing.T) {
	t.Parallel()

	resolution := ResolveModelAuthoringOption("anthropic", ModelAuthoringOption{
		SupportedProviderProtocols: []string{"messages"},
		DefaultProviderProtocol:    "messages",
	})

	if got := resolution.ProtocolOptions(); !reflect.DeepEqual(got, []string{"messages"}) {
		t.Fatalf("protocol options=%v want [messages]", got)
	}
	if got := resolution.DefaultProtocol(); got != "messages" {
		t.Fatalf("default protocol=%q want messages", got)
	}
	if !resolution.SupportsProtocol("messages") {
		t.Fatal("expected explicit option protocol to be supported")
	}
}

func TestResolveModelAuthoringOption_SparseMetadataInheritsProviderManifestProtocols(t *testing.T) {
	t.Parallel()

	resolution := ResolveModelAuthoringOption("openai", ModelAuthoringOption{})
	want := ConcreteProviderProtocolsForSpec("openai")
	if got := resolution.ProtocolOptions(); !reflect.DeepEqual(got, want) {
		t.Fatalf("protocol options=%v want %v", got, want)
	}
	if got := resolution.DefaultProtocol(); got != "" {
		t.Fatalf("default protocol=%q want empty for sparse metadata", got)
	}
}

func TestResolveModelAuthoringOption_AmbiguousMetadataDoesNotAutoSelect(t *testing.T) {
	t.Parallel()

	resolution := ResolveModelAuthoringOption("openai", ModelAuthoringOption{
		SupportedProviderProtocols: []string{"responses", "chat_completions"},
	})
	if got := resolution.DefaultProtocol(); got != "" {
		t.Fatalf("default protocol=%q want empty for ambiguous metadata", got)
	}
	if !resolution.SupportsProtocol("responses") {
		t.Fatal("expected explicit supported protocol to be accepted")
	}
	if resolution.SupportsProtocol("") || resolution.SupportsProtocol("not_a_protocol") {
		t.Fatal("absent or unknown values must not be treated as model-authoring protocols")
	}
}
