package provider

import (
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestTargetSnapshotExecutionProtocolRejectsSplitBrain(t *testing.T) {
	for _, tc := range []struct {
		name        string
		kind        protocolkind.ProtocolKind
		name_       string
		providerDel delivery.Delivery
	}{
		{name: "protocol contradicts kind", kind: protocolkind.Messages, name_: "responses", providerDel: delivery.BufferedDelivery()},
		{name: "streaming protocol with buffered delivery", kind: protocolkind.Responses, name_: "responses_stream", providerDel: delivery.BufferedDelivery()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := NewTargetSnapshot("target", "openai", "https://example.test", "cred", tc.kind, tc.name_, tc.providerDel)
			if err := target.ValidateExecutionProtocol(); err == nil {
				t.Fatalf("incoherent target accepted: %#v", target)
			}
		})
	}
}

func TestTargetSnapshotCarriesOneSemanticProtocol(t *testing.T) {
	target := NewTargetSnapshot("target", "openai", "https://example.test", "cred", protocolkind.Responses, "responses", delivery.BufferedDelivery())
	if err := target.ValidateExecutionProtocol(); err != nil {
		t.Fatal(err)
	}
}

func TestTargetSnapshotAcceptsSemanticInteractionsProtocol(t *testing.T) {
	target := NewTargetSnapshot("target", "gemini", "https://generativelanguage.googleapis.com/v1", "env:GEMINI_API_KEY", protocolkind.Interactions, "interactions_stream", delivery.StreamingDelivery(delivery.FramingSSE))
	if err := target.ValidateExecutionProtocol(); err != nil {
		t.Fatal(err)
	}
}

func TestTargetSnapshotConstructorsExposeExactlyOneProviderOptionsArm(t *testing.T) {
	custom := NewCustomTargetSnapshot("custom", "https://example.test", "cred", protocolkind.Responses, "responses", "X-API-Key", delivery.BufferedDelivery())
	if custom.AuthHeader() != "X-API-Key" {
		t.Fatalf("custom auth header = %q, want %q", custom.AuthHeader(), "X-API-Key")
	}
	if custom.BedrockRegion() != "" {
		t.Fatalf("custom target exposed Bedrock region %q", custom.BedrockRegion())
	}

	bedrock := NewBedrockTargetSnapshot("bedrock", "https://example.test", "cred", protocolkind.Responses, "responses", "us-east-1", delivery.BufferedDelivery())
	if bedrock.BedrockRegion() != "us-east-1" {
		t.Fatalf("Bedrock region = %q, want %q", bedrock.BedrockRegion(), "us-east-1")
	}
	if bedrock.AuthHeader() != "" {
		t.Fatalf("Bedrock target exposed custom auth header %q", bedrock.AuthHeader())
	}
}

func TestGenericTargetSnapshotRejectsProviderSpecificTargets(t *testing.T) {
	for _, providerSpec := range []string{"bedrock", "custom"} {
		t.Run(providerSpec, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("generic constructor admitted provider %q", providerSpec)
				}
			}()
			_ = NewTargetSnapshot("target", providerSpec, "https://example.test", "cred", protocolkind.Responses, "responses", delivery.BufferedDelivery())
		})
	}
}

func TestBedrockTargetSnapshotRejectsMissingSigningRegionAtConstruction(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Bedrock constructor admitted an empty signing region")
		}
	}()
	_ = NewBedrockTargetSnapshot("bedrock", "https://example.test", "cred", protocolkind.Responses, "responses", "", delivery.BufferedDelivery())
}

func TestTargetSnapshotEqualityUsesComparableConcreteOptions(t *testing.T) {
	left := NewBedrockTargetSnapshot("bedrock", "https://example.test", "cred", protocolkind.Responses, "responses", "us-east-1", delivery.BufferedDelivery())
	right := left.Clone()
	if !left.Equal(right) {
		t.Fatal("equal snapshots compare unequal")
	}
}
