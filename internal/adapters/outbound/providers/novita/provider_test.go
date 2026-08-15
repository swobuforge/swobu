package novita

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire"
	"github.com/swobuforge/swobu/internal/wire/messages"
	"github.com/swobuforge/swobu/internal/wire/responses"
)

type credentialResolver struct{}

func (credentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return "novita-token", nil
}

func TestResponsesAndMessagesIngressLowerToNovitaChat(t *testing.T) {
	target := novitaTarget("https://example.test/openai/v1", "provider/exact")
	backend, err := NewRuntime(nil, credentialResolver{}).BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		family protocolkind.ProtocolKind
		decode func(carrier.Document) (wire.ClientDecodeResult, error)
		raw    string
	}{
		{name: "responses", family: protocolkind.Responses, decode: responses.ClientRequestDecoder{}.DecodeClientRequest, raw: `{"model":"provider/exact","input":"hello"}`},
		{name: "messages", family: protocolkind.Messages, decode: messages.ClientRequestDecoder{}.DecodeClientRequest, raw: `{"model":"provider/exact","max_tokens":32,"messages":[{"role":"user","content":"hello"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decoded, err := tc.decode(carrier.NewDocument(tc.family, "application/json", nil, []byte(tc.raw), carrier.Meta{}))
			if err != nil {
				t.Fatal(err)
			}
			document, _, err := backend.Codec.Encode(provider.Request{Canonical: decoded.Request.Request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if string(payload["model"]) != `"provider/exact"` || len(payload["messages"]) == 0 || string(payload["stream"]) != "true" {
				t.Fatalf("%s ingress Chat payload = %#v", tc.name, payload)
			}
		})
	}
}

func TestProfileOwnsOverrideableDerivedChatAndOpenCatalog(t *testing.T) {
	manifest, ok := profile.ProfileForSpec(string(profile.ProviderSpecNovita))
	if !ok {
		t.Fatal("Novita profile is missing")
	}
	if manifest.ProviderDisplayName != "Novita AI" || manifest.ConnectionShape != routing.ConnectionShapeStandard {
		t.Fatalf("display/shape = %q/%v", manifest.ProviderDisplayName, manifest.ConnectionShape)
	}
	if manifest.Locator.Kind != profile.LocatorBaseURL || manifest.Locator.Default != "https://api.novita.ai/openai/v1" {
		t.Fatalf("locator = %#v", manifest.Locator)
	}
	if manifest.Credential.Requirement != profile.CredentialRequired || manifest.Credential.SuggestedEnvVar != "NOVITA_API_KEY" {
		t.Fatalf("credential = %#v", manifest.Credential)
	}
	if manifest.ModelDiscovery != profile.ModelDiscoveryModeAdvisory {
		t.Fatalf("catalog = %v", manifest.ModelDiscovery)
	}
	if got := profile.ConcreteProviderProtocolsForSpec(string(profile.ProviderSpecNovita)); !reflect.DeepEqual(got, []string{"chat_completions_stream"}) {
		t.Fatalf("protocols = %v", got)
	}
	if _, ok := profile.DerivedProtocolForSpec(string(profile.ProviderSpecNovita)); !ok {
		t.Fatal("Novita protocol must be derived")
	}
}

func TestSharedCatalogAndBearerChatUseExactOverrideAndOpenModel(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer novita-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/deployment/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"catalog/model"}]}`))
		case "/deployment/v1/chat/completions":
			var payload map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if string(payload["model"]) != `"deployment/unlisted"` || string(payload["stream"]) != "true" {
				t.Fatalf("model/stream = %s/%s", payload["model"], payload["stream"])
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	target := novitaTarget(server.URL+"/deployment/v1", "deployment/unlisted")
	bundle := NewRuntime(server.Client(), credentialResolver{})
	result, err := bundle.Discovery.ProbeTarget(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Options) != 1 || result.Options[0].Name != "catalog/model" {
		t.Fatalf("catalog = %#v", result.Options)
	}
	backend, err := bundle.BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify(target.Model), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")}})
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Transport.Send(context.Background(), document); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(paths, []string{"/deployment/v1/models", "/deployment/v1/chat/completions"}) {
		t.Fatalf("paths = %v", paths)
	}
}

func TestReasoningDetailsAreCapturedBufferedAndReplayedExactly(t *testing.T) {
	details := `[{"type":"reasoning.text","format":"openai-responses-v1","text":"think"},{"type":"reasoning.text","text":" more"}]`
	document := carrier.NewDocument(protocolkind.ChatCompletions, "application/json", nil, []byte(`{"choices":[{"message":{"role":"assistant","reasoning_content":"readable","reasoning_details":`+details+`,"content":"answer"},"finish_reason":"stop"}]}`), carrier.Meta{})
	extractor := &reasoningDetailsExtractor{}
	cleaned, item, err := protocolcodec.ExtractChatReasoningDocument(document, extractor)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cleaned.RawBytes(), []byte("reasoning_details")) || bytes.Contains(cleaned.RawBytes(), []byte("reasoning_content")) {
		t.Fatalf("Novita carriers leaked: %s", cleaned.RawBytes())
	}
	reasoning, ok := item.Reasoning()
	if !ok || reasoning.Parts()[0].Text() != "readable" {
		t.Fatalf("reasoning item = %#v", item)
	}
	opaque, ok := reasoning.Opaque().ProviderChat(ChatReplayScope)
	if !ok || string(opaque) != details {
		t.Fatalf("opaque = %q, %v; want exact %q", opaque, ok, details)
	}

	replay := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{
		canonicaltest.Message(t, canonical.MessageRoleUser, "question"), item, canonicaltest.Message(t, canonical.MessageRoleAssistant, "answer"),
	}})
	replayDocument, _, err := (reasoningCodec{}).Encode(provider.Request{Canonical: replay, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Messages []map[string]json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(replayDocument.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if got := string(payload.Messages[1]["reasoning_details"]); got != details {
		t.Fatalf("replayed details = %s, want %s", got, details)
	}
}

func TestReasoningDetailsAreCapturedFromFragmentedStream(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"think \"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"reasoning_details\":[{\"type\":\"reasoning.text\",\"text\":\"one\"}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"now\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	extractor := &reasoningDetailsExtractor{}
	body := protocolcodec.NewChatReasoningSSEBody(io.NopCloser(strings.NewReader(stream)), extractor)
	cleaned, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cleaned, []byte("reasoning_details")) || bytes.Contains(cleaned, []byte("reasoning_content")) {
		t.Fatalf("Novita stream carriers leaked: %s", cleaned)
	}
	item, ok := body.Take()
	if !ok {
		t.Fatal("stream reasoning item missing")
	}
	reasoning, _ := item.Reasoning()
	if reasoning.Parts()[0].Text() != "think now" {
		t.Fatalf("stream text = %q", reasoning.Parts()[0].Text())
	}
	opaque, ok := reasoning.Opaque().ProviderChat(ChatReplayScope)
	if !ok || string(opaque) != `[{"type":"reasoning.text","text":"one"}]` {
		t.Fatalf("stream opaque = %q, %v", opaque, ok)
	}
}

func TestNovitaReplayRejectsForeignAndDuplicateState(t *testing.T) {
	foreignOpaque, err := canonical.NewProviderChatOpaqueThinking("other-chat", []byte(`[{}]`))
	if err != nil {
		t.Fatal(err)
	}
	part, _ := canonical.NewReasoningPart(canonical.ReasoningPartTrace, "foreign")
	foreign, _ := canonical.NewReasoningItem([]canonical.ReasoningPart{part}, foreignOpaque)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{foreign, canonicaltest.Message(t, canonical.MessageRoleAssistant, "answer")}})
	document, _, err := (reasoningCodec{}).Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(document.RawBytes(), []byte("reasoning_details")) {
		t.Fatalf("foreign opaque state leaked: %s", document.RawBytes())
	}

	first, _ := canonical.NewProviderChatOpaqueThinking(ChatReplayScope, []byte(`[{"text":"one"}]`))
	second, _ := canonical.NewProviderChatOpaqueThinking(ChatReplayScope, []byte(`[{"text":"two"}]`))
	firstPart, _ := canonical.NewReasoningPart(canonical.ReasoningPartTrace, "one")
	secondPart, _ := canonical.NewReasoningPart(canonical.ReasoningPartTrace, "two")
	firstItem, _ := canonical.NewReasoningItem([]canonical.ReasoningPart{firstPart}, first)
	secondItem, _ := canonical.NewReasoningItem([]canonical.ReasoningPart{secondPart}, second)
	duplicate := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{firstItem, secondItem, canonicaltest.Message(t, canonical.MessageRoleAssistant, "answer")}})
	if _, _, err := (reasoningCodec{}).Encode(provider.Request{Canonical: duplicate, Delivery: delivery.BufferedDelivery()}); err == nil {
		t.Fatal("duplicate Novita replay state must fail")
	}
}

func novitaTarget(baseURL, model string) provider.TargetSnapshot {
	target := provider.NewTargetSnapshot("novita", string(profile.ProviderSpecNovita), baseURL, "env:NOVITA_API_KEY", protocolkind.ChatCompletions, "chat_completions_stream", delivery.StreamingDelivery(delivery.FramingSSE))
	target.Model = model
	return target
}
