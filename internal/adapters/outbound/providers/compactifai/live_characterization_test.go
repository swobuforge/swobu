package compactifai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

var compactifAICharacterizationModels = []string{"quasar-438b", "glm-5-2"}

type compactifAIInferenceEvidence struct {
	Schema       string                                      `json:"schema"`
	Provider     string                                      `json:"provider"`
	ProviderSpec string                                      `json:"provider_spec"`
	Endpoint     string                                      `json:"endpoint"`
	ObservedAt   string                                      `json:"observed_at"`
	Source       string                                      `json:"source"`
	Models       []compactifAIInferenceModelCharacterization `json:"models"`
}

type compactifAIInferenceModelCharacterization struct {
	Model                    string `json:"model"`
	ChatBuffered             bool   `json:"chat_buffered"`
	ChatStreaming            bool   `json:"chat_streaming"`
	ResponsesBuffered        bool   `json:"responses_buffered"`
	ResponsesStreaming       bool   `json:"responses_streaming"`
	ToolLoop                 bool   `json:"tool_loop"`
	ReasoningContentObserved bool   `json:"reasoning_content_observed"`
}

func TestLiveCompactifAISharedRuntime(t *testing.T) {
	if os.Getenv("SWOBU_LIVE_COMPACTIFAI") != "1" {
		t.Skip("set SWOBU_LIVE_COMPACTIFAI=1 to run the bounded live characterization")
	}
	if os.Getenv("COMPACTIFAI_API_KEY") == "" {
		t.Fatal("COMPACTIFAI_API_KEY is required for live CompactifAI characterization")
	}
	evidencePath := os.Getenv("SWOBU_COMPACTIFAI_EVIDENCE_PATH")
	if evidencePath == "" {
		t.Fatal("SWOBU_COMPACTIFAI_EVIDENCE_PATH is required so sanitized evidence has an explicit destination")
	}

	client := &http.Client{Timeout: 120 * time.Second}
	bundle := NewRuntime(client, credentials.NewEnvResolver())
	evidence := compactifAIInferenceEvidence{
		Schema:       "swobu.provider-characterization/v1",
		Provider:     "CompactifAI",
		ProviderSpec: string(profile.ProviderSpecCompactifAI),
		Endpoint:     "https://api.compactif.ai/v1",
		ObservedAt:   time.Now().UTC().Format(time.RFC3339),
		Source:       "live",
		Models:       make([]compactifAIInferenceModelCharacterization, 0, len(compactifAICharacterizationModels)),
	}
	for _, model := range compactifAICharacterizationModels {
		row := compactifAIInferenceModelCharacterization{Model: model}
		var reasoningObserved bool
		row.ChatBuffered = t.Run(model+"/chat_buffered", func(t *testing.T) {
			_, reasoningObserved = characterizeCompactifAIRail(t, bundle.BackendResolver, model, protocolkind.ChatCompletions, "chat_completions", delivery.BufferedDelivery())
		})
		row.ReasoningContentObserved = row.ReasoningContentObserved || reasoningObserved
		row.ChatStreaming = t.Run(model+"/chat_streaming", func(t *testing.T) {
			_, reasoningObserved = characterizeCompactifAIRail(t, bundle.BackendResolver, model, protocolkind.ChatCompletions, "chat_completions_stream", delivery.StreamingDelivery(delivery.FramingSSE))
		})
		row.ReasoningContentObserved = row.ReasoningContentObserved || reasoningObserved
		row.ResponsesBuffered = t.Run(model+"/responses_buffered", func(t *testing.T) {
			_, _ = characterizeCompactifAIRail(t, bundle.BackendResolver, model, protocolkind.Responses, "responses", delivery.BufferedDelivery())
		})
		row.ResponsesStreaming = t.Run(model+"/responses_streaming", func(t *testing.T) {
			_, _ = characterizeCompactifAIRail(t, bundle.BackendResolver, model, protocolkind.Responses, "responses_stream", delivery.StreamingDelivery(delivery.FramingSSE))
		})
		row.ToolLoop = t.Run(model+"/tool_loop", func(t *testing.T) {
			_, reasoningObserved = characterizeCompactifAIToolLoop(t, bundle.BackendResolver, model)
		})
		row.ReasoningContentObserved = row.ReasoningContentObserved || reasoningObserved
		evidence.Models = append(evidence.Models, row)
	}

	raw, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(evidencePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(evidencePath, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCompactifAISharedRuntimeHasSanitizedLiveEvidence(t *testing.T) {
	raw, err := os.ReadFile("testdata/characterization/compactifai-inference-live-2026-09-04.json")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Fatal("missing sanitized CompactifAI live inference evidence")
		}
		t.Fatal(err)
	}
	var evidence compactifAIInferenceEvidence
	if err := json.Unmarshal(raw, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.Schema != "swobu.provider-characterization/v1" || evidence.ProviderSpec != string(profile.ProviderSpecCompactifAI) || evidence.Endpoint != "https://api.compactif.ai/v1" || evidence.Source != "live" {
		t.Fatalf("unexpected CompactifAI inference evidence metadata: %#v", evidence)
	}
	if _, err := time.Parse(time.RFC3339, evidence.ObservedAt); err != nil {
		t.Fatalf("observed_at = %q: %v", evidence.ObservedAt, err)
	}
	if len(evidence.Models) != len(compactifAICharacterizationModels) {
		t.Fatalf("characterized models = %#v", evidence.Models)
	}
	for index, model := range compactifAICharacterizationModels {
		row := evidence.Models[index]
		if row.Model != model || !row.ChatBuffered || !row.ChatStreaming || !row.ResponsesBuffered || !row.ResponsesStreaming || !row.ToolLoop {
			t.Fatalf("incomplete CompactifAI live evidence for %q: %#v", model, row)
		}
	}
}

func characterizeCompactifAIRail(t *testing.T, resolver provider.BackendResolver, model string, kind protocolkind.ProtocolKind, protocol string, mode delivery.Delivery) (bool, bool) {
	t.Helper()
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:    canonical.Specify(model),
		Items:    []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "Reply only OK.")},
		Controls: compactifAILiveControls(t, 24),
	})
	response := sendCompactifAILive(t, resolver, model, kind, protocol, mode, request, "rail")
	return true, responseHasReadableReasoning(response)
}

func characterizeCompactifAIToolLoop(t *testing.T, resolver provider.BackendResolver, model string) (bool, bool) {
	t.Helper()
	toolKey := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "swobu_live_lookup")
	declaration := canonicaltest.MustFunctionTool(toolKey, "Return the supplied query.", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Specify(true))
	firstRequest := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:      canonical.Specify(model),
		Items:      []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, declaration), canonicaltest.Message(t, canonical.MessageRoleUser, "Call swobu_live_lookup with query compactifai. Do not answer directly.")},
		ToolPolicy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicySpecific, &toolKey)),
		Controls:   compactifAILiveControls(t, 128),
	})
	firstResponse := sendCompactifAILive(t, resolver, model, protocolkind.ChatCompletions, "chat_completions", delivery.BufferedDelivery(), firstRequest, "tool-1")
	var toolCall canonical.CanonicalItem
	var callID canonical.ToolCallID
	for _, item := range firstResponse.Items() {
		call, ok := item.ToolCall()
		if ok {
			toolCall = item
			callID = call.CallID()
			break
		}
	}
	if callID.IsZero() {
		t.Fatalf("CompactifAI model %q did not return the required tool call", model)
	}
	result, err := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("compactifai")}, false)
	if err != nil {
		t.Fatal(err)
	}
	secondRequest := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:      canonical.Specify(model),
		Items:      append(firstRequest.Items(), toolCall, result),
		ToolPolicy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicyNone, nil)),
		Controls:   compactifAILiveControls(t, 48),
	})
	secondResponse := sendCompactifAILive(t, resolver, model, protocolkind.ChatCompletions, "chat_completions", delivery.BufferedDelivery(), secondRequest, "tool-2")
	if !responseHasMessage(secondResponse) {
		t.Fatalf("CompactifAI model %q tool loop did not return a final message", model)
	}
	return true, responseHasReadableReasoning(firstResponse) || responseHasReadableReasoning(secondResponse)
}

func sendCompactifAILive(t *testing.T, resolver provider.BackendResolver, model string, kind protocolkind.ProtocolKind, protocol string, mode delivery.Delivery, canonicalRequest canonical.CanonicalRequest, suffix string) *canonical.CanonicalResponse {
	t.Helper()
	target := compactifAITarget("https://api.compactif.ai/v1", model, kind, protocol, mode)
	backend, err := resolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	toolNames, _, err := provider.BuildAttemptToolNames(canonicalRequest)
	if err != nil {
		t.Fatal(err)
	}
	request := provider.Request{Attempt: provider.AttemptContext{ExchangeID: "compactifai-live-" + model + "-" + suffix}, Canonical: canonicalRequest, ToolNames: toolNames, Delivery: mode}
	document, _, err := backend.Codec.Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	ingress, err := backend.Transport.Send(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := backend.Codec.Decode(context.Background(), request, ingress)
	if err != nil {
		t.Fatal(err)
	}
	binding := canonical.ResponseBinding{SwobuID: canonical.NewSwobuResponseID("compactifai-live-" + model + "-" + suffix), TargetID: target.TargetID, TargetVersion: target.TargetVersion}
	response, err := canonical.ProjectStream(context.Background(), canonical.NewBoundResponseIdentityStream(decoded.Stream, binding), binding)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func compactifAILiveControls(t *testing.T, maxOutputTokens int) canonical.GenerationControls {
	t.Helper()
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{MaxOutputTokens: &maxOutputTokens})
	if err != nil {
		t.Fatal(err)
	}
	return controls
}

func responseHasReadableReasoning(response *canonical.CanonicalResponse) bool {
	for _, item := range response.Items() {
		reasoning, ok := item.Reasoning()
		if ok && len(reasoning.Parts()) > 0 && reasoning.Opaque().IsZero() {
			return true
		}
	}
	return false
}

func responseHasMessage(response *canonical.CanonicalResponse) bool {
	for _, item := range response.Items() {
		if message, ok := item.Message(); ok && len(message.Content()) > 0 {
			return true
		}
	}
	return false
}
