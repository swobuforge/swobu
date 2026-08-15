package deepinfra

import (
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
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

type credentialResolver struct{}

func (credentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return "deepinfra-token", nil
}

func TestRuntimeUsesDerivedChatCompletionsOnly(t *testing.T) {
	bundle := NewRuntime(http.DefaultClient, credentialResolver{})
	target := deepInfraTarget("https://api.deepinfra.com/v1/openai")
	backend, err := bundle.BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := backend.Codec.(failFastCodec); !ok {
		t.Fatalf("codec = %T, want DeepInfra fail-fast codec", backend.Codec)
	}
	for _, kind := range []protocolkind.ProtocolKind{protocolkind.Responses, protocolkind.Messages} {
		invalid := target.Clone()
		invalid.ProtocolKind = kind
		if _, err := bundle.BackendResolver.ResolveBackend(invalid); err == nil {
			t.Fatalf("%s must not resolve as a DeepInfra backend", kind)
		}
	}
}

func TestTransportUsesChatPathAndBearerCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/openai/chat/completions" {
			t.Fatalf("method/path = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer deepinfra-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	backend, err := NewRuntime(server.Client(), credentialResolver{}).BackendResolver.ResolveBackend(deepInfraTarget(server.URL + "/v1/openai"))
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("future:model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
	})
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Transport.Send(context.Background(), document); err != nil {
		t.Fatal(err)
	}
}

func TestFailFastFollowsTransientRouteContext(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("deploy_id:private"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
	})
	codec := failFastCodec{standard: protocolCodec()}
	for _, tc := range []struct {
		name string
		next bool
		want bool
	}{
		{name: "fallback candidate", next: true, want: true},
		{name: "terminal target", next: false, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, _, err := codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery(), EncodeContext: provider.EncodeContext{HasNextRouteCandidate: tc.next}})
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(doc.RawBytes(), &payload); err != nil {
				t.Fatal(err)
			}
			got, present := payload["fail_fast"].(bool)
			if present != tc.want || got != tc.want {
				t.Fatalf("fail_fast = %v (present %t), want %t", got, present, tc.want)
			}
		})
	}
}

func TestFailFastOverloadIsRejectedBeforeExecutionOnlyForDocumentedResponse(t *testing.T) {
	overloaded := canonical.NewBackendError("deepinfra", http.StatusTooManyRequests, `{"error":{"code":"engine_overloaded"}}`, "")
	for _, tc := range []struct {
		name     string
		document string
		err      error
		want     provider.ExecutionPossibility
	}{
		{name: "documented overload", document: `{"fail_fast":true}`, err: provider.AttemptMayHaveExecuted(overloaded), want: provider.ExecutionRejectedBeforeExecution},
		{name: "terminal overload", document: `{}`, err: provider.AttemptMayHaveExecuted(overloaded), want: provider.ExecutionMayHaveOccurred},
		{name: "unmarked fail fast", document: `{"fail_fast":true}`, err: provider.AttemptMayHaveExecuted(overloaded), want: provider.ExecutionMayHaveOccurred},
		{name: "other rate limit", document: `{"fail_fast":true}`, err: provider.AttemptMayHaveExecuted(canonical.NewBackendError("deepinfra", http.StatusTooManyRequests, `{"error":{"code":"rate_limited"}}`, "")), want: provider.ExecutionMayHaveOccurred},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := overloadTransport{standard: provider.TransportFunc(func(context.Context, carrier.Document) (provider.Ingress, error) {
				return nil, tc.err
			})}
			meta := carrier.Meta{}
			if tc.name != "terminal overload" && tc.name != "unmarked fail fast" {
				meta.Opaque = map[string]string{failFastCarrierMarker: "true"}
			}
			_, err := transport.Send(context.Background(), carrier.NewDocument(protocolkind.ChatCompletions, "application/json", nil, []byte(tc.document), meta))
			failure, ok := provider.AsAttemptFailure(err)
			if !ok || failure.Execution() != tc.want {
				t.Fatalf("failure = %#v, want execution %v", err, tc.want)
			}
		})
	}
}

func TestDiscoveryUsesOfficialCatalogAndFiltersTextGeneration(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.String() != "https://api.deepinfra.com/models/list" {
			t.Fatalf("request = %s %s", req.Method, req.URL)
		}
		if req.Header.Get("Authorization") != "Bearer deepinfra-token" {
			t.Fatalf("authorization = %q", req.Header.Get("Authorization"))
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`[
			{"id":"text/a:1","reported_type":"text-generation"},
			{"id":"embedding","reported_type":"embedding"},
			{"model_name":"text/b","reported_type":"text-generation"}
		]`)), Request: req}, nil
	})}
	result, err := NewRuntime(client, credentialResolver{}).Discovery.ProbeTarget(context.Background(), deepInfraTarget("ignored"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Options) != 2 || result.Options[0].Name != "text/a:1" || result.Options[1].Name != "text/b" {
		t.Fatalf("deployments = %#v", result.Options)
	}
}

func deepInfraTarget(baseURL string) provider.TargetSnapshot {
	target := provider.NewTargetSnapshot("deepinfra", "deepinfra", baseURL, "env:DEEPINFRA_TOKEN", protocolkind.ChatCompletions, "chat_completions", delivery.BufferedDelivery())
	target.Model = "future:model"
	return target
}

func protocolCodec() protocolcodec.Codec {
	return protocolcodec.Codec{Protocol: protocolkind.ChatCompletions}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

var _ providersruntime.Discovery = NewRuntime(nil, nil).Discovery
