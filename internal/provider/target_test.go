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
