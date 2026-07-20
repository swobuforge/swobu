package openai

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestOpenAIRequestMutationPreservesRawJSONIntegers(t *testing.T) {
	target := provider.NewTargetSnapshot("backend", string(profile.ProviderSpecOpenAI), "https://api.openai.com/v1", "env:TOKEN", protocolkind.ChatCompletions, "", "chat_completions")
	target.Model = "model"
	backend, err := NewRuntime(nil, nil).BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: canonicaltest.LargeIntegerRequest(t, "model"), Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(document.RawBytes(), []byte("9007199254740993")); got != 3 {
		t.Fatalf("large integer occurrences = %d, want 3: %s", got, document.RawBytes())
	}
}

func TestNewRuntime_BindsOpenAIProviderID(t *testing.T) {
	rt := NewRuntime(nil, nil)
	if rt.ProviderID != profile.ProviderSpecOpenAI {
		t.Fatalf("provider id = %s, want %s", rt.ProviderID, profile.ProviderSpecOpenAI)
	}
}

func TestRuntimeOwnsOpenAIChatCompletionsTokenSpelling(t *testing.T) {
	maxTokens := 64
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{MaxOutputTokens: &maxTokens})
	if err != nil {
		t.Fatal(err)
	}
	target := provider.NewTargetSnapshot("backend", string(profile.ProviderSpecOpenAI), "https://api.openai.com/v1", "env:TOKEN", protocolkind.ChatCompletions, "", "chat_completions")
	target.Model = "model"
	backend, err := NewRuntime(nil, nil).BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")}, Controls: controls,
	}), Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["max_completion_tokens"] != float64(maxTokens) {
		t.Fatalf("max_completion_tokens = %#v, want %d", payload["max_completion_tokens"], maxTokens)
	}
	if _, exists := payload["max_tokens"]; exists {
		t.Fatalf("standard spelling leaked through OpenAI dialect: %s", document.RawBytes())
	}
}
