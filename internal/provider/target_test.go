package provider

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestTargetSnapshotExecutionProtocolRejectsSplitBrain(t *testing.T) {
	for _, tc := range []struct {
		name  string
		kind  protocolkind.ProtocolKind
		frame string
		name_ string
	}{
		{name: "protocol contradicts kind", kind: protocolkind.Messages, frame: executionFrameHTTPJSONBody, name_: "responses"},
		{name: "stream protocol contradicts frame", kind: protocolkind.Responses, frame: executionFrameHTTPJSONBody, name_: "responses_stream"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := NewTargetSnapshot("target", "openai", "https://example.test", "cred", tc.kind, tc.frame, tc.name_)
			if err := target.ValidateExecutionProtocol(); err == nil {
				t.Fatalf("incoherent target accepted: %#v", target)
			}
		})
	}
}

func TestTargetSnapshotConstructorDerivesOneCoherentFrame(t *testing.T) {
	target := NewTargetSnapshot("target", "openai", "https://example.test", "cred", protocolkind.Responses, "", "responses_stream")
	if target.SelectedFrame != executionFrameSSEEvent {
		t.Fatalf("selected frame = %q, want %q", target.SelectedFrame, executionFrameSSEEvent)
	}
	if err := target.ValidateExecutionProtocol(); err != nil {
		t.Fatal(err)
	}
}

func TestTargetSnapshotConstructorsExposeExactlyOneProviderOptionsArm(t *testing.T) {
	custom := NewCustomTargetSnapshot("custom", "https://example.test", "cred", protocolkind.Responses, "", "responses", "X-API-Key")
	if custom.AuthHeader() != "X-API-Key" {
		t.Fatalf("custom auth header = %q, want %q", custom.AuthHeader(), "X-API-Key")
	}
	if custom.BedrockRegion() != "" {
		t.Fatalf("custom target exposed Bedrock region %q", custom.BedrockRegion())
	}

	bedrock := NewBedrockTargetSnapshot("bedrock", "https://example.test", "cred", protocolkind.Responses, "", "responses", "us-east-1")
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
			_ = NewTargetSnapshot("target", providerSpec, "https://example.test", "cred", protocolkind.Responses, "", "responses")
		})
	}
}

func TestBedrockTargetSnapshotRejectsMissingSigningRegionAtConstruction(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Bedrock constructor admitted an empty signing region")
		}
	}()
	_ = NewBedrockTargetSnapshot("bedrock", "https://example.test", "cred", protocolkind.Responses, "", "responses", "")
}

func TestTargetSnapshotEqualityUsesComparableConcreteOptions(t *testing.T) {
	left := NewBedrockTargetSnapshot("bedrock", "https://example.test", "cred", protocolkind.Responses, "", "responses", "us-east-1")
	right := left.Clone()
	if !left.Equal(right) {
		t.Fatal("equal snapshots compare unequal")
	}
}
