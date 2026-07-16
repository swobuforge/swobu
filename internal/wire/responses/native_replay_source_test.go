package responses

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/replay"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestNativeReplaySource_ResponsesCodecImplements(t *testing.T) {
	var _ wire.NativeReplaySource = ProviderDocumentDecoder{}
	var _ wire.NativeReplaySource = ProviderEnvelopeDecoder{}
}

func TestNativeReplaySource_ExtractsProviderID(t *testing.T) {
	target := replay.TargetKey{
		ProviderSpec:     "openai",
		Protocol:         protocolkind.Responses,
		ProviderProtocol: "responses",
		BaseURL:          "https://api.openai.com",
		AuthScope:        "cred-1",
		ModelID:          "gpt-4o",
	}
	native := ProviderDocumentDecoder{}.NativeReplayFromOutput(
		target,
		replay.ID("swobu_1"),
		"provider_resp_99",
	)
	if native == nil {
		t.Fatal("expected non-nil NativeRef for output with result ID")
	}
	if native.ReplayID != replay.ID("swobu_1") {
		t.Fatalf("native.ReplayID=%q, want swobu_1", native.ReplayID)
	}
	if native.Target != target {
		t.Fatalf("native.Target=%+v, want %+v", native.Target, target)
	}
	if native.Kind != replay.NativeRefProviderResponseID {
		t.Fatalf("native.Kind=%q, want %q", native.Kind, replay.NativeRefProviderResponseID)
	}
	if native.Value != "provider_resp_99" {
		t.Fatalf("native.Value=%q, want provider_resp_99", native.Value)
	}
}

func TestNativeReplaySource_ReturnsNilForEmptyResultID(t *testing.T) {
	native := ProviderDocumentDecoder{}.NativeReplayFromOutput(
		replay.TargetKey{},
		replay.ID("swobu_1"),
		"",
	)
	if native != nil {
		t.Fatalf("expected nil NativeRef for empty result ID, got %+v", native)
	}
}

func TestCanonicalOutput_WithResultID(t *testing.T) {
	output := canonical.NewConversationOutput("old_id", "m", []canonical.CanonicalItem{
		canonical.NewTextOutputItem("text_1", "hello"),
	}, "completed")

	mutated := output.WithResultID("new_swobu_id")
	if mutated.ResultID() != "new_swobu_id" {
		t.Fatalf("ResultID=%q, want new_swobu_id", mutated.ResultID())
	}
	// Original should be unchanged (value semantics).
	if output.ResultID() != "old_id" {
		t.Fatalf("original ResultID was mutated: %q", output.ResultID())
	}
}
