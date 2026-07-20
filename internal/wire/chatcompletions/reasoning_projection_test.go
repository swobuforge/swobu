package chatcompletions

import (
	"bytes"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestBufferedChatProjectionOmitsReasoningAndRecordsPortableDrop(t *testing.T) {
	part, err := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "brief")
	if err != nil {
		t.Fatal(err)
	}
	reasoning, err := canonical.NewReasoningItem([]canonical.ReasoningPart{part}, canonical.OpaqueThinking{})
	if err != nil {
		t.Fatal(err)
	}
	response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: "resp"}, "model", []canonical.CanonicalItem{reasoning}, "stop", canonical.NewUnknownTokenUsage())
	if err != nil {
		t.Fatal(err)
	}
	result, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(result.Document.RawBytes(), []byte("brief")) {
		t.Fatalf("standard Chat exposed reasoning: %s", result.Document.RawBytes())
	}
	if len(result.Decisions) != 1 || result.Decisions[0].Feature != compat.ResponseItemsReasoning || result.Decisions[0].Outcome != compat.Drop {
		t.Fatalf("decisions = %#v", result.Decisions)
	}
}
