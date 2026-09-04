package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/continuity"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/cachelocality"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/testkit/providertest"
)

func TestOpenAIRequestMutationPreservesRawJSONIntegers(t *testing.T) {
	target := provider.NewTargetSnapshot("backend", string(profile.ProviderSpecOpenAI), "https://api.openai.com/v1", "env:TOKEN", protocolkind.ChatCompletions, "chat_completions", delivery.BufferedDelivery())
	target.Model = "model"
	backend, err := NewRuntime(nil, nil).BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	request := canonicaltest.LargeIntegerRequest(t, "model")
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, ToolNames: names, Delivery: delivery.BufferedDelivery()})
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

func TestOpenAIEmitsPromptCacheKeyAcrossOfficialProtocols(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
	})
	for _, protocol := range []protocolkind.ProtocolKind{protocolkind.ChatCompletions, protocolkind.Responses} {
		t.Run(string(protocol), func(t *testing.T) {
			target := provider.NewTargetSnapshot("backend", string(profile.ProviderSpecOpenAI), "https://api.openai.com/v1", "env:TOKEN", protocol, string(protocol), delivery.BufferedDelivery())
			target.Model = "model"
			backend, err := NewRuntime(nil, nil).BackendResolver.ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			document, _, err := backend.Codec.Encode(provider.Request{Attempt: provider.AttemptContext{CacheLocality: cachelocality.Explicit(" Raw Key ")}, Canonical: request, Delivery: delivery.BufferedDelivery()})
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload["prompt_cache_key"] != " Raw Key " {
				t.Fatalf("prompt_cache_key = %#v", payload["prompt_cache_key"])
			}
			without, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
			if err != nil {
				t.Fatal(err)
			}
			var zeroPayload map[string]any
			if err := json.Unmarshal(without.RawBytes(), &zeroPayload); err != nil {
				t.Fatal(err)
			}
			if _, exists := zeroPayload["prompt_cache_key"]; exists {
				t.Fatalf("zero cache locality emitted prompt_cache_key: %s", without.RawBytes())
			}
		})
	}
}

func TestOpenAIBoundsPromptCacheKeyAcrossOfficialProtocols(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
	})
	longKey := cachelocality.Derived("workspace", "lineage").Key()
	for _, protocol := range []protocolkind.ProtocolKind{protocolkind.ChatCompletions, protocolkind.Responses} {
		t.Run(string(protocol), func(t *testing.T) {
			target := provider.NewTargetSnapshot("backend", string(profile.ProviderSpecOpenAI), "https://api.openai.com/v1", "env:TOKEN", protocol, string(protocol), delivery.BufferedDelivery())
			target.Model = "model"
			backend, err := NewRuntime(nil, nil).BackendResolver.ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			project := func(key string) string {
				document, _, err := backend.Codec.Encode(provider.Request{Attempt: provider.AttemptContext{CacheLocality: cachelocality.Explicit(key)}, Canonical: request, Delivery: delivery.BufferedDelivery()})
				if err != nil {
					t.Fatal(err)
				}
				var payload map[string]any
				if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
					t.Fatal(err)
				}
				projected, _ := payload["prompt_cache_key"].(string)
				return projected
			}
			bounded := project(longKey)
			if len(bounded) != 64 || bounded == longKey {
				t.Fatalf("bounded prompt_cache_key = %q (length %d)", bounded, len(bounded))
			}
			if repeated := project(longKey); repeated != bounded {
				t.Fatalf("bounded prompt_cache_key is unstable: %q / %q", bounded, repeated)
			}
			if different := project(longKey + "y"); different == bounded {
				t.Fatalf("different long cache locality collapsed to %q", bounded)
			}
			unicodeBoundary := strings.Repeat("é", 64)
			if projected := project(unicodeBoundary); projected != unicodeBoundary {
				t.Fatalf("64-character key changed: %q", projected)
			}
		})
	}
}

func TestOpenAICacheSensitiveRenderingIgnoresExecutionContext(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
	})
	for _, protocol := range []protocolkind.ProtocolKind{protocolkind.ChatCompletions, protocolkind.Responses} {
		t.Run(string(protocol), func(t *testing.T) {
			target := provider.NewTargetSnapshot("backend", string(profile.ProviderSpecOpenAI), "https://api.openai.com/v1", "env:TOKEN", protocol, string(protocol), delivery.BufferedDelivery())
			target.Model = "model"
			backend, err := NewRuntime(nil, nil).BackendResolver.ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			project := func(exchangeID, locality string, d delivery.Delivery) []byte {
				document, _, err := backend.Codec.Encode(provider.Request{Attempt: provider.AttemptContext{ExchangeID: exchangeID, CacheLocality: cachelocality.Explicit(locality)}, Canonical: request, Delivery: d})
				if err != nil {
					t.Fatal(err)
				}
				projection, err := providertest.CacheSensitiveProjection(document)
				if err != nil {
					t.Fatal(err)
				}
				return projection
			}
			first := project("a", "locality-a", delivery.BufferedDelivery())
			second := project("b", "locality-b", delivery.BufferedDelivery())
			streamed := project("c", "locality-c", delivery.StreamingDelivery(delivery.FramingSSE))
			if !bytes.Equal(first, second) || !bytes.Equal(first, streamed) {
				t.Fatalf("OpenAI cache-sensitive projection changed: %s / %s / %s", first, second, streamed)
			}
		})
	}
}

func TestRuntimeOwnsOpenAIChatCompletionsTokenSpelling(t *testing.T) {
	maxTokens := 64
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{MaxOutputTokens: &maxTokens})
	if err != nil {
		t.Fatal(err)
	}
	target := provider.NewTargetSnapshot("backend", string(profile.ProviderSpecOpenAI), "https://api.openai.com/v1", "env:TOKEN", protocolkind.ChatCompletions, "chat_completions", delivery.BufferedDelivery())
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

func TestOpenAIResponsesPreservesExplicitMaxOutputTokensEight(t *testing.T) {
	target := provider.NewTargetSnapshot("backend", string(profile.ProviderSpecOpenAI), "https://api.openai.com/v1", "env:TOKEN", protocolkind.Responses, "responses", delivery.BufferedDelivery())
	target.Model = "model"
	backend, err := NewRuntime(nil, nil).BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	encode := func(maxOutputTokens *int) map[string]any {
		t.Helper()
		controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{MaxOutputTokens: maxOutputTokens})
		if err != nil {
			t.Fatal(err)
		}
		document, changes, err := backend.Codec.Encode(provider.Request{Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")}, Controls: controls,
		}), Delivery: delivery.BufferedDelivery()})
		if err != nil {
			t.Fatal(err)
		}
		if len(changes) != 0 {
			t.Fatalf("OpenAI Responses max-output projection changes = %#v", changes)
		}
		var payload map[string]any
		if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}

	eight := 8
	if got := encode(&eight)["max_output_tokens"]; got != float64(8) {
		t.Fatalf("max_output_tokens = %#v, want 8", got)
	}
	if value, exists := encode(nil)["max_output_tokens"]; exists {
		t.Fatalf("unspecified max_output_tokens was invented: %#v", value)
	}
}

func TestRuntimeUsesSharedOfficialResponsesToolLowering(t *testing.T) {
	childKey, _ := canonical.NewToolKey("workspace", canonical.ToolKindFunction, "read_file")
	child := canonicaltest.MustFunctionTool(childKey, "Read", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	namespaceKey, _ := canonical.NewRequestToolKey(canonical.ToolKindNamespace, "workspace")
	namespace, _ := canonical.NewToolNamespace(namespaceKey, "Workspace", []canonical.ToolDeclaration{child})
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, namespace), canonicaltest.Message(t, canonical.MessageRoleUser, "read")},
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	target := provider.NewTargetSnapshot("backend", string(profile.ProviderSpecOpenAI), "https://api.openai.com/v1", "env:TOKEN", protocolkind.Responses, "responses", delivery.BufferedDelivery())
	target.Model = "model"
	backend, err := NewRuntime(nil, nil).BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, ToolNames: names, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	wireName, _ := names.WireName(childKey)
	if raw := string(document.RawBytes()); bytes.Contains(document.RawBytes(), []byte(`"type":"namespace"`)) || !bytes.Contains(document.RawBytes(), []byte(`"name":"`+wireName+`"`)) {
		t.Fatalf("OpenAI Responses document = %s", raw)
	}
}

func TestOpenAIResponsesUsesStableHostedWebSearch(t *testing.T) {
	declaration := canonical.NewWebSearchDeclaration()
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"),
		Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, declaration), canonicaltest.Message(t, canonical.MessageRoleUser, "search")},
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	target := provider.NewTargetSnapshot("backend", string(profile.ProviderSpecOpenAI), "https://api.openai.com/v1", "env:TOKEN", protocolkind.Responses, "responses", delivery.BufferedDelivery())
	target.Model = "model"
	backend, err := NewRuntime(nil, nil).BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, ToolNames: names, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	raw := document.RawBytes()
	if !bytes.Contains(raw, []byte(`"type":"web_search"`)) || bytes.Contains(raw, []byte("web_search_preview")) {
		t.Fatalf("OpenAI Responses hosted search is not stable dialect: %s", raw)
	}
	if !bytes.Contains(raw, []byte(`"include":["reasoning.encrypted_content","web_search_call.action.sources"]`)) {
		t.Fatalf("OpenAI Responses lacks source disclosure include: %s", raw)
	}
}

func TestOfficialOpenAIRuntimeCapturesStoredResponsesContinuation(t *testing.T) {
	var firstWire string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		firstWire = string(body)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"id":"provider_response_1","model":"gpt-test","status":"completed","store":true,"output":[{"type":"message","id":"msg_1","status":"completed","role":"assistant","content":[{"type":"output_text","text":"answer one","annotations":[]}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
	}))
	defer server.Close()

	target := provider.NewTargetSnapshot("official-openai", string(profile.ProviderSpecOpenAI), server.URL, "env:TOKEN", protocolkind.Responses, "responses", delivery.BufferedDelivery())
	target.Model = "gpt-test"
	backend, err := NewRuntime(server.Client(), staticCredentialProvider{}).BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	turnOne := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gpt-test"),
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "turn one")},
	})
	firstRequest := provider.Request{Attempt: provider.AttemptContext{ExchangeID: "official-openai-capture"}, Canonical: turnOne, Delivery: delivery.BufferedDelivery()}
	firstDocument, _, err := backend.Codec.Encode(firstRequest)
	if err != nil {
		t.Fatal(err)
	}
	ingress, err := backend.Transport.Send(context.Background(), firstDocument)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := backend.Codec.Decode(context.Background(), firstRequest, ingress)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(decoded.Stream, canonical.ResponseBinding{
		SwobuID: "swobu_previous", TargetID: backend.Target.TargetID, TargetVersion: backend.Target.TargetVersion,
	}), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	turnOneResponse, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	continuation := turnOneResponse.Response().Responses
	if continuation == nil || continuation.ProviderResponseID != "provider_response_1" || continuation.TargetID != backend.Target.TargetID || continuation.TargetVersion != backend.Target.TargetVersion {
		t.Fatalf("captured continuation = %#v", continuation)
	}
	if !strings.Contains(firstWire, "turn one") {
		t.Fatalf("first request missing turn one: %s", firstWire)
	}

	turnTwo := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:            canonical.Specify("gpt-test"),
		Items:            []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "turn two")},
		PreviousResponse: &canonical.ResponseRef{SwobuID: "swobu_previous"},
	})
	prepared, err := continuity.Resume(turnTwo, continuity.Checkpoint{HistoryScheme: "responses/v1", Request: turnOne, Response: *turnOneResponse})
	if err != nil {
		t.Fatal(err)
	}
	previous, ok := prepared.PreviousHistory(backend.Target.TargetID, backend.Target.TargetVersion)
	if !ok {
		t.Fatal("captured official continuation was not available on resume")
	}
	secondDocument, _, err := backend.Codec.Encode(provider.Request{
		Canonical: prepared.Request(), Delivery: delivery.BufferedDelivery(),
		PreviousHistory: &previous,
	})
	if err != nil {
		t.Fatal(err)
	}
	secondWire := string(secondDocument.RawBytes())
	if !strings.Contains(secondWire, `"previous_response_id":"provider_response_1"`) || strings.Contains(secondWire, "turn one") || strings.Contains(secondWire, "answer one") || !strings.Contains(secondWire, "turn two") {
		t.Fatalf("second request did not lower captured continuation correctly: %s", secondWire)
	}
}

type staticCredentialProvider struct{}

func (staticCredentialProvider) ResolveCredential(context.Context, string, string) (string, error) {
	return "token_test", nil
}
