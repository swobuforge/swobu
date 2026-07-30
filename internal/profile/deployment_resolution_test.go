package profile

import (
	"reflect"
	"testing"
)

func TestResolveProviderDeployment_ExplicitFactsWinOverProviderDefaults(t *testing.T) {
	t.Parallel()

	resolution := ResolveProviderDeployment("anthropic", ProviderDeploymentRecord{
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
		t.Fatal("expected explicit deployment protocol to be supported")
	}
}

func TestResolveProviderDeployment_SparseMetadataInheritsProviderManifestProtocols(t *testing.T) {
	t.Parallel()

	resolution := ResolveProviderDeployment("openai", ProviderDeploymentRecord{})
	want := ConcreteProviderProtocolsForSpec("openai")
	if got := resolution.ProtocolOptions(); !reflect.DeepEqual(got, want) {
		t.Fatalf("protocol options=%v want %v", got, want)
	}
	if got := resolution.DefaultProtocol(); got != "" {
		t.Fatalf("default protocol=%q want empty for sparse metadata", got)
	}
}

func TestResolveProviderDeployment_AmbiguousMetadataDoesNotAutoSelect(t *testing.T) {
	t.Parallel()

	resolution := ResolveProviderDeployment("openai", ProviderDeploymentRecord{
		SupportedProviderProtocols: []string{"responses", "chat_completions"},
	})
	if got := resolution.DefaultProtocol(); got != "" {
		t.Fatalf("default protocol=%q want empty for ambiguous metadata", got)
	}
	if !resolution.SupportsProtocol("responses") {
		t.Fatal("expected explicit supported protocol to be accepted")
	}
	if resolution.SupportsProtocol(ProviderProtocolAuto) {
		t.Fatal("auto must not be treated as a deployment protocol")
	}
}
