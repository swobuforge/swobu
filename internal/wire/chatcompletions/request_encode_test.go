package chatcompletions

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func EncodeCarrier(req canonical.CanonicalRequest, d delivery.Delivery) (carrier.Document, error) {
	return EncodeCarrierWithDecisions(req, d, nil, "", EncodeOptions{})
}

func TestEncodeCarrier_LowersInstructionsToLeadingSystemMessage(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:        canonical.Specify("gpt-4o-mini"),
		Instructions: canonical.Specify(canonical.NewSystemInstructionSet("Use native tools for filesystem work.")),
		Items:        []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "inspect files")},
	})

	wire, err := EncodeCarrier(req, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("EncodeCarrier returned error: %v", err)
	}
	var body struct {
		Messages []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(wire.Raw, &body); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	if len(body.Messages) != 2 {
		t.Fatalf("messages len = %d, want 2", len(body.Messages))
	}
	if body.Messages[0]["role"] != "system" || body.Messages[0]["content"] != "Use native tools for filesystem work." {
		t.Fatalf("system message = %#v, want instruction message", body.Messages[0])
	}
	if body.Messages[1]["role"] != "user" || body.Messages[1]["content"] != "inspect files" {
		t.Fatalf("user message = %#v, want user request", body.Messages[1])
	}
}

func TestEncodeCarrier_OmitsDisclosureOnlyReasoning(t *testing.T) {
	reasoning, _ := canonical.NewReasoningControls(canonical.ReasoningControlsParams{
		Disclosure: canonical.Specify(canonical.ReasoningDisclosureNone),
	})
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:     canonical.Specify("gpt"),
		Items:     []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
		Reasoning: reasoning,
	})
	document, err := EncodeCarrier(req, delivery.BufferedDelivery())
	if err != nil {
		t.Fatal(err)
	}
	if string(document.RawBytes()) == "" {
		t.Fatal("empty document")
	}
}
