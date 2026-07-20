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

func TestPreviousResponseEmitsExplicitEmptySpecifiedBands(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"), Instructions: canonical.Specify(canonical.InstructionSet{}), Tools: canonicaltest.SpecifiedToolSet(t),
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
	if _, ok := payload["instructions"]; !ok {
		t.Fatal("explicit empty instructions omitted")
	}
	if tools, ok := payload["tools"].([]any); !ok || len(tools) != 0 {
		t.Fatalf("explicit empty tools=%#v", payload["tools"])
	}
}
