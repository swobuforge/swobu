package responses

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestPreviousResponseUsesTypedProviderResponseRef(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "continue")}, PreviousResponse: testResponsesPrevious("swobu_1", "provider_1")})
	doc, err := EncodeCarrier(request, delivery.BufferedDelivery())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(doc.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["previous_response_id"] != "provider_1" {
		t.Fatalf("previous_response_id=%#v", payload["previous_response_id"])
	}
}

func TestPreviousResponseDoesNotSynthesizeDeletedContextBands(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:      canonical.Specify("m"),
		ToolPolicy: canonical.Specify(canonical.ToolPolicy{}), ToolCallBatch: canonical.Specify(canonical.ToolCallBatchPolicy{}), OutputFormat: canonical.Specify(canonical.OutputFormat{}),
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "continue")}, PreviousResponse: testResponsesPrevious("swobu_1", "provider_1"),
	})
	doc, err := EncodeCarrier(request, delivery.BufferedDelivery())
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(doc.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["instructions"]; ok {
		t.Fatalf("deleted root instructions synthesized: %#v", payload)
	}
	if _, ok := payload["tools"]; ok {
		t.Fatalf("deleted root tools synthesized: %#v", payload)
	}
}
