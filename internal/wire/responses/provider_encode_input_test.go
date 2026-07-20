package responses

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestEncode_UsesResponsesRefinementWhenPresent(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:            canonical.Specify("m"),
		Items:            []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
		PreviousResponse: testResponsesPrevious("swobu_resp_123", "provider_resp_789"),
	})

	// The typed Responses refinement supplies the provider wire ID; the Swobu ID
	// remains the replay/client identity.
	input := wire.ProviderEncodeInput{Request: req}
	result, err := ProviderRequestDocumentEncoder{}.EncodeProviderRequestDocument(input, delivery.BufferedDelivery(), "ex-1")
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(result.Document.RawBytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := body["previous_response_id"]; got != "provider_resp_789" {
		t.Fatalf("previous_response_id=%q, want provider_resp_789", got)
	}
}

func TestEncode_RejectsSwobuSelectorWithoutResponsesRefinement(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:            canonical.Specify("m"),
		Items:            []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
		PreviousResponse: &canonical.ResponseRef{SwobuID: "swobu_resp_123"},
	})

	input := wire.ProviderEncodeInput{Request: req}
	if _, err := (ProviderRequestDocumentEncoder{}).EncodeProviderRequestDocument(input, delivery.BufferedDelivery(), "ex-2"); err == nil {
		t.Fatal("encode succeeded with an unhydrated Swobu response selector")
	}
}

func TestEncode_RejectsMalformedResponsesRefinement(t *testing.T) {
	for _, tc := range []struct {
		name   string
		native canonical.ResponsesNativeRef
	}{
		{name: "empty provider response ID", native: canonical.ResponsesNativeRef{TargetID: "target-a", TargetVersion: 1}},
		{name: "empty target ID", native: canonical.ResponsesNativeRef{ProviderResponseID: "provider_resp_789", TargetVersion: 1}},
		{name: "zero target version", native: canonical.ResponsesNativeRef{ProviderResponseID: "provider_resp_789", TargetID: "target-a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := canonical.NewCanonicalRequest(canonical.RequestParams{
				Model: canonical.Specify("m"),
				PreviousResponse: &canonical.ResponseRef{
					SwobuID: "swobu_resp_123", Responses: &tc.native,
				},
			})
			input := wire.ProviderEncodeInput{Request: req}
			if _, err := (ProviderRequestDocumentEncoder{}).EncodeProviderRequestDocument(input, delivery.BufferedDelivery(), "ex-invalid"); err == nil {
				t.Fatal("encode succeeded with malformed Responses native refinement")
			}
		})
	}
}

func TestClientResponseExposesSwobuIDAndNeverProviderID(t *testing.T) {
	output := canonicaltest.ResponseWithRef(t,
		canonical.ResponseRef{SwobuID: "swobu_resp_123", Responses: &canonical.ResponsesNativeRef{
			ProviderResponseID: "provider_resp_789", TargetID: "target-a", TargetVersion: 1,
		}},
		"m", nil, "completed", canonical.NewUnknownTokenUsage(),
	)
	result, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(output)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(result.Document.RawBytes())
	if !strings.Contains(raw, `"id":"swobu_resp_123"`) || strings.Contains(raw, "provider_resp_789") {
		t.Fatalf("client response identity leaked provider domain: %s", raw)
	}
}

func testResponsesPrevious(swobuID, providerID string) *canonical.ResponseRef {
	return &canonical.ResponseRef{SwobuID: canonical.SwobuResponseID(swobuID), Responses: &canonical.ResponsesNativeRef{
		ProviderResponseID: canonical.NewResponsesNativeResponseID(providerID), TargetID: "target-test", TargetVersion: 1,
	}}
}

func TestEncode_EncodesCanonicalRequestAlways(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("test-model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleUser, "hello"),
			canonicaltest.Message(t, canonical.MessageRoleAssistant, "world"),
		},
	})

	input := wire.ProviderEncodeInput{Request: req}
	result, err := ProviderRequestDocumentEncoder{}.EncodeProviderRequestDocument(input, delivery.BufferedDelivery(), "ex-3")
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(result.Document.RawBytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["model"] != "test-model" {
		t.Fatalf("model=%q, want test-model", body["model"])
	}
	if body["input"] == nil {
		t.Fatalf("input should be present for non-empty thread")
	}
}
