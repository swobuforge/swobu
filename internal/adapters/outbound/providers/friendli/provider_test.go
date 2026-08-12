package friendli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

type credentialResolver struct{}

func (credentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return "friendli-token", nil
}

func TestRuntimeComposesSharedProtocolsAndManualDiscovery(t *testing.T) {
	bundle := NewRuntime(http.DefaultClient, credentialResolver{})
	for _, kind := range []protocolkind.ProtocolKind{protocolkind.ChatCompletions, protocolkind.Responses, protocolkind.Messages} {
		target := provider.NewTargetSnapshot("friendli", "friendli", "https://friendli-gateway.example/v1", "", kind, profile.FrameSSEEvent, string(kind)+"_stream")
		target.Model = "exact-route"
		backend, err := bundle.BackendResolver.ResolveBackend(target)
		if err != nil {
			t.Fatalf("resolve %s: %v", kind, err)
		}
		if kind == protocolkind.ChatCompletions {
			if _, ok := backend.Codec.(reasoningCodec); !ok {
				t.Fatalf("Chat codec = %T, want Friendli reasoning codec", backend.Codec)
			}
		}
	}
	if _, err := bundle.Discovery.ProbeTarget(context.Background(), provider.TargetSnapshot{}); err == nil {
		t.Fatal("manual Friendli model authoring must not probe a fake universal catalog")
	}
}

func TestTransportUsesExactEndpointAndOptionalBearerCredential(t *testing.T) {
	for _, tc := range []struct {
		name       string
		baseURL    string
		credential string
		wantPath   string
		wantAuth   string
	}{
		{name: "container no credential", baseURL: "/container/v1", wantPath: "/container/v1/chat/completions"},
		{name: "gateway bearer credential", baseURL: "/gateway/v1", credential: "env:FRIENDLI_TOKEN", wantPath: "/gateway/v1/chat/completions", wantAuth: "Bearer friendli-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.wantPath || r.Header.Get("Authorization") != tc.wantAuth {
					t.Fatalf("path/auth = %s/%q, want %s/%q", r.URL.Path, r.Header.Get("Authorization"), tc.wantPath, tc.wantAuth)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
			}))
			defer srv.Close()
			bundle := NewRuntime(srv.Client(), credentialResolver{})
			target := provider.NewTargetSnapshot("friendli", "friendli", srv.URL+tc.baseURL, tc.credential, protocolkind.ChatCompletions, "", "chat_completions_stream")
			target.Model = "exact-route"
			backend, err := bundle.BackendResolver.ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("exact-route"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")}})
			doc, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := backend.Transport.Send(context.Background(), doc); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReasoningCodecProjectsOnlyCanonicalDisclosureAndForwardsSharedFields(t *testing.T) {
	disclosure := canonical.ReasoningDisclosureNone
	reasoning, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{Disclosure: canonical.Specify(disclosure)})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")}, Reasoning: reasoning})
	doc, _, err := (reasoningCodec{}).Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	payload := decodePayload(t, doc.RawBytes())
	if payload["parse_reasoning"] != true || payload["include_reasoning"] != false {
		t.Fatalf("Friendli disclosure projection = %#v", payload)
	}

	ordinary := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")}})
	doc, _, err = (reasoningCodec{}).Encode(provider.Request{Canonical: ordinary, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	payload = decodePayload(t, doc.RawBytes())
	if _, present := payload["parse_reasoning"]; present {
		t.Fatalf("Friendli invented disclosure fields without canonical intent: %#v", payload)
	}
}

func TestReasoningCodecPreservesReadableReasoningWithoutOpaqueReplay(t *testing.T) {
	document := carrierDocument(`{"choices":[{"message":{"role":"assistant","reasoning_content":"think first","content":"answer"},"finish_reason":"stop"}]}`)
	cleaned, item, err := protocolcodec.ExtractChatReasoningDocument(document, friendliChatReasoningExtractor{})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cleaned.RawBytes(), []byte("reasoning_content")) {
		t.Fatalf("provider-only field leaked to shared decoder: %s", cleaned.RawBytes())
	}
	reasoning, ok := item.Reasoning()
	if !ok || !reasoning.Opaque().IsZero() {
		t.Fatalf("reasoning item = %#v, want readable non-replay reasoning", item)
	}
}

func TestStreamingReasoningIsRemovedAndCapturedWithoutOpaqueReplay(t *testing.T) {
	raw := "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"think \"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{\"reasoning_content\":\"now\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"answer\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"
	body := protocolcodec.NewChatReasoningSSEBody(io.NopCloser(strings.NewReader(raw)), friendliChatReasoningExtractor{})
	cleaned, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cleaned, []byte("reasoning_content")) {
		t.Fatalf("provider-only field leaked to shared decoder: %s", cleaned)
	}
	item, ok := body.Take()
	if !ok {
		t.Fatal("streamed reasoning item missing")
	}
	reasoning, _ := item.Reasoning()
	if !reasoning.Opaque().IsZero() {
		t.Fatal("Friendli readable reasoning must not become opaque replay state")
	}
}

func decodePayload(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func carrierDocument(raw string) carrier.Document {
	return carrier.NewDocument(protocolkind.ChatCompletions, "application/json", nil, []byte(raw), carrier.Meta{})
}

var _ providersruntime.Discovery = NewRuntime(nil, nil).Discovery
