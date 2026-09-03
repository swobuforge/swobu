package chatcompletions

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func EncodeCarrier(req canonical.CanonicalRequest, d delivery.Delivery) (carrier.Document, error) {
	names, _, err := provider.BuildAttemptToolNames(req)
	if err != nil {
		return carrier.Document{}, err
	}
	return EncodeCarrierWithChanges(req, names, d, nil, "")
}

func TestEncodeCarrier_LowersInstructionsToLeadingSystemMessage(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gpt-4o-mini"),
		Items: []canonical.CanonicalItem{canonicaltest.MustInstruction(canonical.MessageRoleSystem, "Use native tools for filesystem work."), canonicaltest.Message(t, canonical.MessageRoleUser, "inspect files")},
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

func TestCompileProviderRequestDocumentRequestsStreamingUsageWhenAccepted(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gpt"),
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
	})
	for _, test := range []struct {
		name    string
		accepts bool
		want    bool
	}{
		{name: "accepted", accepts: true, want: true},
		{name: "rejected", accepts: false, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			document, err := CompileProviderRequestDocument(req, nil, delivery.StreamingDelivery(delivery.FramingSSE), nil, "", CompileOptions{
				Lowering: DefaultLowering(), AcceptsStreamIncludeUsage: func() bool { return test.accepts },
			})
			if err != nil {
				t.Fatal(err)
			}
			_, present := document.Payload["stream_options"]
			if present != test.want {
				t.Fatalf("stream_options present = %t, want %t: %#v", present, test.want, document.Payload)
			}
		})
	}
}

func TestEncodeCarrier_CurrentHostedSearchRequiresExactTargetLowering(t *testing.T) {
	set, err := canonical.NewToolSet([]canonical.ToolDeclaration{canonical.NewWebSearchDeclaration()})
	if err != nil {
		t.Fatal(err)
	}
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, set.Declarations()...), canonicaltest.Message(t, canonical.MessageRoleUser, "search")},
	})

	for _, tc := range []struct {
		name     string
		delivery delivery.Delivery
	}{
		{name: "buffered", delivery: delivery.BufferedDelivery()},
		{name: "streaming", delivery: delivery.StreamingDelivery(delivery.FramingSSE)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var changes []compat.Change
			document, err := EncodeCarrierWithChanges(req, nil, tc.delivery, &changes, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(document.RawBytes()) == 0 || len(changes) != 1 || changes[0].Kind != compat.Omission {
				t.Fatalf("document=%s changes=%#v", document.RawBytes(), changes)
			}
		})
	}
}

func TestApplyAttemptDecoration_RejectsReservedSemanticKeys(t *testing.T) {
	reserved := []string{"model", "messages", "tools", "tool_choice", "stream", "temperature", "top_p", "max_tokens", "max_completion_tokens", "stop", "response_format"}
	for _, key := range reserved {
		t.Run(key, func(t *testing.T) {
			payload := map[string]any{"model": "test"}
			err := ApplyAttemptDecoration(payload, map[string]any{key: "forbidden"})
			if err == nil {
				t.Fatalf("expected error when decorating reserved key %q", key)
			}
		})
	}

	payload := map[string]any{"model": "test"}
	if err := ApplyAttemptDecoration(payload, map[string]any{"service_tier": "auto", "route_tag": "blue"}); err != nil {
		t.Fatalf("unexpected error decorating non-semantic keys: %v", err)
	}
	if payload["service_tier"] != "auto" || payload["route_tag"] != "blue" {
		t.Fatalf("decoration not applied: %#v", payload)
	}
}

func TestCompileProviderRequestDocument_RejectsReasoningMutatingNonReasoningFields(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gpt"),
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
	})

	forbiddenNonReasoning := []string{
		"model", "messages", "tools", "tool_choice", "parallel_tool_calls",
		"functions", "function_call", "stream", "stream_options",
		"temperature", "top_p", "max_tokens", "max_completion_tokens",
		"stop", "response_format", "n", "presence_penalty",
		"frequency_penalty", "seed", "user", "logprobs", "top_logprobs",
		"logit_bias", "modalities",
	}

	for _, field := range forbiddenNonReasoning {
		t.Run("Forbids_"+field, func(t *testing.T) {
			badReasoning := func(req canonical.CanonicalRequest, _ ReasoningTargetDialect, changeLog *[]compat.Change, exchangeID string) (map[string]any, error) {
				return map[string]any{field: "override"}, nil
			}
			_, err := CompileProviderRequestDocument(req, nil, delivery.BufferedDelivery(), nil, "", CompileOptions{
				Lowering: DefaultLowering().Overlay(Lowering{Reasoning: badReasoning}),
			})
			if err == nil {
				t.Fatalf("expected error when LowerReasoning mutates non-reasoning field %q", field)
			}
		})
	}

	// Known reasoning fields and unknown provider-private reasoning carriers are permitted.
	allowedReasoning := func(req canonical.CanonicalRequest, _ ReasoningTargetDialect, changeLog *[]compat.Change, exchangeID string) (map[string]any, error) {
		return map[string]any{
			"reasoning_effort":        "high",
			"thinking":                map[string]any{"type": "enabled", "budget_tokens": 1024},
			"custom_provider_carrier": "provider_specific_value",
		}, nil
	}
	doc, err := CompileProviderRequestDocument(req, nil, delivery.BufferedDelivery(), nil, "", CompileOptions{
		Lowering: DefaultLowering().Overlay(Lowering{Reasoning: allowedReasoning}),
	})
	if err != nil {
		t.Fatalf("unexpected error with valid reasoning fields: %v", err)
	}
	if doc.Payload["reasoning_effort"] != "high" || doc.Payload["custom_provider_carrier"] != "provider_specific_value" {
		t.Fatalf("allowed reasoning fields not set in payload: %#v", doc.Payload)
	}
}
