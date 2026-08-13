package responses

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestPreviousResponseUsesTypedProviderResponseRef(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "continue")}})
	doc, err := EncodeCarrierWithChanges(EncodeInput{Request: request, PreviousHistory: responsesPrevious("provider_1"), ToolNames: testAttemptToolNames(request)}, delivery.BufferedDelivery(), nil, "", EncodeOptions{})
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
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "continue")},
	})
	doc, err := EncodeCarrierWithChanges(EncodeInput{Request: request, PreviousHistory: responsesPrevious("provider_1"), ToolNames: testAttemptToolNames(request)}, delivery.BufferedDelivery(), nil, "", EncodeOptions{})
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

func TestPreviousResponseIgnoresInteractionsContinuation(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "continue")}})
	doc, err := EncodeCarrierWithChanges(EncodeInput{Request: request, PreviousHistory: &provider.PreviousHistory{
		Response:  canonical.ResponseRef{Interactions: &canonical.InteractionsContinuation{ProviderInteractionID: canonical.NewInteractionID("interaction_1"), TargetID: "target", TargetVersion: 1}},
		OmitStart: 0,
		OmitEnd:   1,
	}, ToolNames: testAttemptToolNames(request)}, delivery.BufferedDelivery(), nil, "", EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(doc.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if _, present := payload["previous_response_id"]; present {
		t.Fatalf("Responses wire accepted foreign native continuation: %#v", payload)
	}
}

func responsesPrevious(providerID string) *provider.PreviousHistory {
	return &provider.PreviousHistory{Response: canonical.ResponseRef{Responses: &canonical.ResponsesContinuation{ProviderResponseID: canonical.NewResponsesResponseID(providerID), TargetID: "target", TargetVersion: 1}}}
}
