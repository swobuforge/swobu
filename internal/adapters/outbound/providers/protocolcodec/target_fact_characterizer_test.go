package protocolcodec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/executionaffinity"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestCharacterizeTargetFactUsesValidIsolatedFixturesForEveryFact(t *testing.T) {
	tests := []struct {
		name     string
		fact     provider.TargetFact
		protocol protocolkind.Kind
		codec    Codec
	}{
		{name: "parallel tool calls false", fact: provider.AcceptsParallelToolCallsFalse, protocol: protocolkind.ChatCompletions},
		{name: "max completion tokens", fact: provider.AcceptsMaxCompletionTokens, protocol: protocolkind.ChatCompletions, codec: Codec{ChatDialect: ChatDialect{UseMaxCompletionTokens: true}}},
		{name: "reasoning effort max", fact: provider.AcceptsReasoningEffortMax, protocol: protocolkind.Responses},
		{name: "reasoning disabled", fact: provider.AcceptsReasoningDisabled, protocol: protocolkind.Responses},
		{name: "reasoning context all turns", fact: provider.AcceptsResponsesReasoningContextAllTurns, protocol: protocolkind.Responses},
		{name: "function call output array", fact: provider.AcceptsFunctionCallOutputArray, protocol: protocolkind.Responses},
		{name: "chat stream include usage", fact: provider.AcceptsChatStreamIncludeUsage, protocol: protocolkind.ChatCompletions},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			codec := test.codec
			codec.Protocol = test.protocol
			target := provider.TargetSnapshot{
				ProviderSpec: "custom", TargetID: "target", TargetVersion: 1,
				Model: "model", ProtocolKind: test.protocol,
			}
			request, ok := targetFactFixture(target.Model, test.fact)
			if !ok {
				t.Fatal("fixture construction failed")
			}
			if err := canonical.ValidateMaterializedRequest(request); err != nil {
				t.Fatalf("fixture is invalid canonical: %v", err)
			}
			preferred := encodeTargetFactFixture(t, codec, request, targetFactFixtureDelivery(test.fact), test.fact, true)
			control := encodeTargetFactFixture(t, codec, request, targetFactFixtureDelivery(test.fact), test.fact, false)
			if bytes.Equal(preferred, control) {
				t.Fatal("preferred and control fixtures do not differ")
			}
			assertTargetFactFixtureDiffersOnlyAtOwnedOccurrence(t, test.fact, preferred, control)

			t.Run("preferred completion resolves true", func(t *testing.T) {
				calls := 0
				resolution := codec.CharacterizeTargetFact(context.Background(), target, test.fact, provider.TransportFunc(func(_ context.Context, _ carrier.Document) (provider.Ingress, error) {
					calls++
					return completedTargetFactIngress(test.protocol, targetFactFixtureDelivery(test.fact)), nil
				}))
				if !resolution.Conclusive || !resolution.Value || calls != 1 {
					t.Fatalf("resolution = %#v calls=%d", resolution, calls)
				}
			})

			t.Run("preferred rejection control completion and matching preferred rejection resolve false", func(t *testing.T) {
				calls := 0
				resolution := codec.CharacterizeTargetFact(context.Background(), target, test.fact, provider.TransportFunc(func(_ context.Context, _ carrier.Document) (provider.Ingress, error) {
					calls++
					if calls != 2 {
						return nil, targetFactStructuredRejection("reasoning.context")
					}
					return completedTargetFactIngress(test.protocol, targetFactFixtureDelivery(test.fact)), nil
				}))
				if !resolution.Conclusive || resolution.Value || calls != 3 {
					t.Fatalf("resolution = %#v calls=%d", resolution, calls)
				}
			})

			t.Run("changed restored rejection is inconclusive", func(t *testing.T) {
				calls := 0
				resolution := codec.CharacterizeTargetFact(context.Background(), target, test.fact, provider.TransportFunc(func(_ context.Context, _ carrier.Document) (provider.Ingress, error) {
					calls++
					switch calls {
					case 1:
						return nil, targetFactStructuredRejection("reasoning.context")
					case 2:
						return completedTargetFactIngress(test.protocol, targetFactFixtureDelivery(test.fact)), nil
					default:
						return nil, targetFactStructuredRejection("different.param")
					}
				}))
				if resolution.Conclusive || calls != 3 {
					t.Fatalf("resolution = %#v calls=%d", resolution, calls)
				}
			})

			t.Run("other preferred result is inconclusive", func(t *testing.T) {
				calls := 0
				resolution := codec.CharacterizeTargetFact(context.Background(), target, test.fact, provider.TransportFunc(func(_ context.Context, _ carrier.Document) (provider.Ingress, error) {
					calls++
					return nil, errors.New("transport unavailable")
				}))
				if resolution.Conclusive || calls != 1 {
					t.Fatalf("resolution = %#v calls=%d", resolution, calls)
				}
			})

			t.Run("other control result is inconclusive", func(t *testing.T) {
				calls := 0
				resolution := codec.CharacterizeTargetFact(context.Background(), target, test.fact, provider.TransportFunc(func(_ context.Context, _ carrier.Document) (provider.Ingress, error) {
					calls++
					return nil, targetFactStructuredRejection("reasoning.context")
				}))
				if resolution.Conclusive || calls != 2 {
					t.Fatalf("resolution = %#v calls=%d", resolution, calls)
				}
			})

			t.Run("matching status-only rejection resolves false", func(t *testing.T) {
				calls := 0
				resolution := codec.CharacterizeTargetFact(context.Background(), target, test.fact, provider.TransportFunc(func(_ context.Context, _ carrier.Document) (provider.Ingress, error) {
					calls++
					if calls == 2 {
						return completedTargetFactIngress(test.protocol, targetFactFixtureDelivery(test.fact)), nil
					}
					return nil, provider.AttemptMayHaveExecuted(provider.Rejected(canonical.NewBackendError("target", 400, "opaque rejection", "")))
				}))
				if !resolution.Conclusive || resolution.Value || calls != 3 {
					t.Fatalf("resolution = %#v calls=%d", resolution, calls)
				}
			})

			t.Run("matching message-only structured rejection resolves false", func(t *testing.T) {
				calls := 0
				resolution := codec.CharacterizeTargetFact(context.Background(), target, test.fact, provider.TransportFunc(func(_ context.Context, _ carrier.Document) (provider.Ingress, error) {
					calls++
					if calls == 2 {
						return completedTargetFactIngress(test.protocol, targetFactFixtureDelivery(test.fact)), nil
					}
					backendErr := canonical.NewStructuredBackendError("target", test.protocol, http.StatusBadRequest, canonical.BackendErrorDetail{
						Message: "provider prose only",
					}, "")
					return nil, provider.AttemptMayHaveExecuted(provider.Rejected(backendErr))
				}))
				if !resolution.Conclusive || resolution.Value || calls != 3 {
					t.Fatalf("resolution = %#v calls=%d", resolution, calls)
				}
			})

			t.Run("changed restored status is inconclusive", func(t *testing.T) {
				calls := 0
				resolution := codec.CharacterizeTargetFact(context.Background(), target, test.fact, provider.TransportFunc(func(_ context.Context, _ carrier.Document) (provider.Ingress, error) {
					calls++
					if calls == 2 {
						return completedTargetFactIngress(test.protocol, targetFactFixtureDelivery(test.fact)), nil
					}
					status := http.StatusBadRequest
					if calls == 3 {
						status = http.StatusUnprocessableEntity
					}
					return nil, provider.AttemptMayHaveExecuted(provider.Rejected(canonical.NewBackendError("target", status, "opaque rejection", "")))
				}))
				if resolution.Conclusive || calls != 3 {
					t.Fatalf("resolution = %#v calls=%d", resolution, calls)
				}
			})
		})
	}
}

func TestCharacterizeTargetFactReusesOneSyntheticThreadIdentity(t *testing.T) {
	var projected []executionaffinity.Key
	codec := Codec{
		Protocol: protocolkind.ChatCompletions,
		ProjectRequestHeaders: func(attempt provider.AttemptContext, _ http.Header) error {
			projected = append(projected, attempt.ExecutionAffinity)
			return nil
		},
	}
	target := provider.TargetSnapshot{ProviderSpec: "custom", TargetID: "target", TargetVersion: 7, Model: "model", ProtocolKind: protocolkind.ChatCompletions}
	calls := 0
	resolution := codec.CharacterizeTargetFact(context.Background(), target, provider.AcceptsParallelToolCallsFalse, provider.TransportFunc(func(_ context.Context, _ carrier.Document) (provider.Ingress, error) {
		calls++
		if calls != 2 {
			return nil, targetFactStructuredRejection("parallel_tool_calls")
		}
		return completedTargetFactIngress(protocolkind.ChatCompletions, delivery.BufferedDelivery()), nil
	}))
	if !resolution.Conclusive || resolution.Value {
		t.Fatalf("resolution = %#v, want conclusive false", resolution)
	}
	if len(projected) != 3 || projected[0].IsZero() || projected[0] != projected[1] || projected[1] != projected[2] {
		t.Fatal("preferred, control, and restored characterization did not share one synthetic execution affinity")
	}
}

func targetFactStructuredRejection(param string) error {
	backendErr := canonical.NewStructuredBackendError("target", protocolkind.Responses, http.StatusBadRequest, canonical.BackendErrorDetail{
		Type: "invalid_request_error", Code: "unsupported_value", Param: param,
	}, "")
	return provider.AttemptMayHaveExecuted(provider.Rejected(backendErr))
}

func assertTargetFactFixtureDiffersOnlyAtOwnedOccurrence(t *testing.T, fact provider.TargetFact, preferred, control []byte) {
	t.Helper()
	decode := func(raw []byte) map[string]any {
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}
	normalize := func(payload map[string]any) {
		switch fact {
		case provider.AcceptsParallelToolCallsFalse:
			delete(payload, "parallel_tool_calls")
		case provider.AcceptsMaxCompletionTokens:
			delete(payload, "max_completion_tokens")
			delete(payload, "max_tokens")
		case provider.AcceptsReasoningEffortMax, provider.AcceptsReasoningDisabled:
			if reasoning, ok := payload["reasoning"].(map[string]any); ok {
				delete(reasoning, "effort")
				if len(reasoning) == 0 {
					delete(payload, "reasoning")
				}
			}
		case provider.AcceptsResponsesReasoningContextAllTurns:
			if reasoning, ok := payload["reasoning"].(map[string]any); ok {
				delete(reasoning, "context")
				if len(reasoning) == 0 {
					delete(payload, "reasoning")
				}
			}
		case provider.AcceptsFunctionCallOutputArray:
			if input, ok := payload["input"].([]any); ok {
				for _, rawItem := range input {
					if item, ok := rawItem.(map[string]any); ok && item["type"] == "function_call_output" {
						delete(item, "output")
					}
				}
			}
		case provider.AcceptsChatStreamIncludeUsage:
			delete(payload, "stream_options")
		default:
			t.Fatalf("missing fixture-delta normalizer for fact %v", fact)
		}
	}
	preferredPayload, controlPayload := decode(preferred), decode(control)
	normalize(preferredPayload)
	normalize(controlPayload)
	if !reflect.DeepEqual(preferredPayload, controlPayload) {
		t.Fatalf("fixture differs outside fact %v:\npreferred=%#v\ncontrol=%#v", fact, preferredPayload, controlPayload)
	}
}

func encodeTargetFactFixture(t *testing.T, codec Codec, request canonical.CanonicalRequest, fixtureDelivery delivery.Delivery, fact provider.TargetFact, value bool) []byte {
	t.Helper()
	facts := provider.NewTargetFacts(func(read provider.TargetFact) (bool, bool) {
		if read != fact {
			t.Fatalf("fixture read fact %v, want %v", read, fact)
		}
		return value, true
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := codec.Encode(provider.Request{
		Canonical: request, TargetFacts: facts, ToolNames: names, Delivery: fixtureDelivery,
	})
	if err != nil {
		t.Fatal(err)
	}
	reads := facts.Reads()
	if len(reads) != 1 || reads[fact] != value {
		t.Fatalf("fixture reads = %#v, want only %v=%t", reads, fact, value)
	}
	return document.RawBytes()
}

func completedTargetFactIngress(protocol protocolkind.Kind, fixtureDelivery delivery.Delivery) provider.Ingress {
	if fixtureDelivery.IsStreaming() {
		raw := "data: {\"id\":\"response\",\"model\":\"model\",\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n" +
			"data: {\"id\":\"response\",\"model\":\"model\",\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n" +
			"data: {\"id\":\"response\",\"model\":\"model\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: [DONE]\n\n"
		return provider.StreamIngress{Stream: carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}}
	}
	raw := []byte(`{"id":"response","model":"model","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	if protocol == protocolkind.Responses {
		raw = []byte(`{"id":"response","model":"model","status":"completed","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}]}`)
	}
	return provider.DocumentIngress{Document: carrier.NewDocument(protocol, "application/json", nil, raw, carrier.Meta{})}
}
