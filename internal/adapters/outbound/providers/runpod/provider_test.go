package runpod

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

type credentialResolver struct{}

func (credentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return "runpod-token", nil
}

func TestProfileOwnsRunpodConnectionAndAllSharedProtocols(t *testing.T) {
	manifest, ok := profile.ProfileForSpec(string(profile.ProviderSpecRunPod))
	if !ok {
		t.Fatal("Runpod profile is missing")
	}
	if manifest.ProviderDisplayName != "Runpod" || manifest.ConnectionShape != routing.ConnectionShapeStandard {
		t.Fatalf("display/shape = %q/%v", manifest.ProviderDisplayName, manifest.ConnectionShape)
	}
	if manifest.Locator.Kind != profile.LocatorBaseURL || manifest.Locator.Label != "endpoint" {
		t.Fatalf("locator = %#v", manifest.Locator)
	}
	if manifest.Credential.Requirement != profile.CredentialOptional || manifest.Credential.SuggestedEnvVar != "RUNPOD_API_KEY" {
		t.Fatalf("credential = %#v", manifest.Credential)
	}
	want := []string{"responses", "responses_stream", "chat_completions", "chat_completions_stream", "messages", "messages_stream"}
	if got := profile.ConcreteProviderProtocolsForSpec(string(profile.ProviderSpecRunPod)); !reflect.DeepEqual(got, want) {
		t.Fatalf("protocols = %v, want %v", got, want)
	}
}

func TestRunpodEndpointNormalizationFinalizesStandardConnection(t *testing.T) {
	connection, err := routing.FinalizeConnection(routing.ConnectionDraft{
		Provider: "runpod",
		Standard: &routing.StandardConnectionDraft{Locator: "abc123", Credential: "env:RUNPOD_API_KEY"},
	}, profile.RoutingConstructionFacts())
	if err != nil {
		t.Fatal(err)
	}
	standard, ok := connection.(routing.StandardConnection)
	if !ok {
		t.Fatalf("Runpod connection type = %T, want routing.StandardConnection", connection)
	}
	locator, ok := standard.Locator()
	if !ok || locator.String() != "https://api.runpod.ai/v2/abc123/openai/v1" {
		t.Fatalf("Runpod locator = %q/%t", locator.String(), ok)
	}
}

func TestRunpodEndpointNormalizationIsPureAndRejectsMalformedAbsoluteInputs(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  string
		want string
	}{
		{name: "endpoint id", raw: "abc123", want: "https://api.runpod.ai/v2/abc123/openai/v1"},
		{name: "public slug", raw: "qwen3-32b-awq", want: "https://api.runpod.ai/v2/qwen3-32b-awq/openai/v1"},
		{name: "escaped token", raw: "team/endpoint", want: "https://api.runpod.ai/v2/team%2Fendpoint/openai/v1"},
		{name: "load balancer URL", raw: "https://abc.api.runpod.ai/v1/", want: "https://abc.api.runpod.ai/v1"},
		{name: "pod proxy URL", raw: "https://pod-8000.proxy.runpod.net/v1", want: "https://pod-8000.proxy.runpod.net/v1"},
		{name: "URL query and fragment", raw: "https://example.test/v1?tenant=one#route", want: "https://example.test/v1?tenant=one#route"},
		{name: "URL query ending in slash", raw: "https://example.test/v1?tenant=one/", want: "https://example.test/v1?tenant=one/"},
		{name: "URL path slash before query", raw: "https://example.test/v1/?tenant=one/", want: "https://example.test/v1?tenant=one/"},
		{name: "encoded path slash", raw: "https://example.test/v1%2F/", want: "https://example.test/v1%2F"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := profile.NormalizeRunPodEndpoint(test.raw)
			if err != nil || got != test.want {
				t.Fatalf("NormalizeRunPodEndpoint(%q) = %q, %v; want %q", test.raw, got, err, test.want)
			}
		})
	}
	for _, raw := range []string{"", "ftp://example.test/v1", "https://", "https://user:pass@example.test/v1", "http://bad host/v1", "https://example.test/v1\nignored"} {
		t.Run("reject "+raw, func(t *testing.T) {
			if got, err := profile.NormalizeRunPodEndpoint(raw); err == nil || got != "" {
				t.Fatalf("NormalizeRunPodEndpoint(%q) = %q, %v; want rejection", raw, got, err)
			}
		})
	}
}

func TestRunpodDiscoveryUsesOptionalBearerAndOpenCatalogOutcomes(t *testing.T) {
	for _, test := range []struct {
		name          string
		status        int
		body          string
		credentialRef string
		wantAuth      string
		wantModels    []string
		wantError     bool
	}{
		{name: "authenticated catalog", status: http.StatusOK, body: `{"data":[{"id":"served-alias"},{"id":"served-alias"},{"id":"second"}]}`, credentialRef: "env:RUNPOD_API_KEY", wantAuth: "Bearer runpod-token", wantModels: []string{"second", "served-alias"}},
		{name: "anonymous catalog", status: http.StatusOK, body: `{"data":[]}`, wantModels: []string{}},
		{name: "empty endpoint catalog", status: http.StatusNotFound, wantModels: []string{}},
		{name: "method unavailable catalog", status: http.StatusMethodNotAllowed, wantModels: []string{}},
		{name: "auth failure", status: http.StatusUnauthorized, wantError: true},
		{name: "forbidden", status: http.StatusForbidden, wantError: true},
		{name: "rate limited", status: http.StatusTooManyRequests, wantError: true},
		{name: "server failure", status: http.StatusBadGateway, wantError: true},
		{name: "malformed success", status: http.StatusOK, body: `not-json`, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
					t.Fatalf("method/path = %s %s", r.Method, r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != test.wantAuth {
					t.Fatalf("Authorization = %q, want %q", got, test.wantAuth)
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()

			result, err := NewRuntime(server.Client(), credentialResolver{}).Discovery.ProbeTarget(context.Background(), runpodTarget(server.URL+"/v1", test.credentialRef, protocolkind.Responses, "responses"))
			if test.wantError {
				if err == nil {
					t.Fatalf("discovery error = nil, want failure")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := make([]string, 0, len(result.Options))
			for _, deployment := range result.Options {
				got = append(got, deployment.Name)
			}
			if !reflect.DeepEqual(got, test.wantModels) {
				t.Fatalf("models = %v, want %v", got, test.wantModels)
			}
		})
	}
}

func TestRunpodSharedProtocolsUseOnlySelectedPathAndDelivery(t *testing.T) {
	protocols := []struct {
		name string
		kind protocolkind.ProtocolKind
		path string
	}{
		{name: "responses", kind: protocolkind.Responses, path: "/v1/responses"},
		{name: "chat_completions", kind: protocolkind.ChatCompletions, path: "/v1/chat/completions"},
		{name: "messages", kind: protocolkind.Messages, path: "/v1/messages"},
	}
	for _, protocol := range protocols {
		protocol := protocol
		for _, providerDelivery := range []delivery.Delivery{delivery.BufferedDelivery(), delivery.StreamingDelivery(delivery.FramingSSE)} {
			providerDelivery := providerDelivery
			t.Run(protocol.name+"/"+string(providerDelivery.Mode), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != protocol.path {
						t.Fatalf("path = %q, want %q", r.URL.Path, protocol.path)
					}
					if r.Header.Get("Authorization") != "Bearer runpod-token" {
						t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
					}
					if r.Header.Get("User-Agent") != "swobu/dev" {
						t.Fatalf("User-Agent = %q", r.Header.Get("User-Agent"))
					}
					var payload map[string]json.RawMessage
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
					if len(payload["model"]) == 0 {
						t.Fatalf("model missing from %s request", r.URL.Path)
					}
					streaming := len(payload["stream"]) > 0 && string(payload["stream"]) == "true"
					if streaming {
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = w.Write([]byte("data: {}\n\ndata: [DONE]\n\n"))
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{}`))
				}))
				defer server.Close()

				target := runpodTarget(server.URL+"/v1", "env:RUNPOD_API_KEY", protocol.kind, protocol.name)
				backend, err := NewRuntime(server.Client(), credentialResolver{}).BackendResolver.ResolveBackend(target)
				if err != nil {
					t.Fatal(err)
				}
				request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify(target.Model), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")}})
				document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: providerDelivery})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := backend.Transport.Send(context.Background(), document); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestRunpodOptionalFieldsReachBackendWithoutCapabilityPreflight(t *testing.T) {
	dispatched := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatched = true
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		for _, field := range []string{"messages", "tools", "response_format"} {
			if len(payload[field]) == 0 {
				t.Fatalf("shared Chat field %q missing: %#v", field, payload)
			}
		}
		if !strings.Contains(string(payload["messages"]), "image_url") {
			t.Fatalf("vision content missing: %s", payload["messages"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	request, names := runpodOptionalRequest(t)
	target := runpodTarget(server.URL+"/v1", "", protocolkind.ChatCompletions, "chat_completions")
	backend, err := NewRuntime(server.Client(), credentialResolver{}).BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, ToolNames: names, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatalf("local capability preflight rejected request: %v", err)
	}
	if _, err := backend.Transport.Send(context.Background(), document); err != nil {
		t.Fatal(err)
	}
	if !dispatched {
		t.Fatal("optional request never reached Runpod")
	}
}

func runpodTarget(baseURL, credential string, kind protocolkind.ProtocolKind, protocolName string) provider.TargetSnapshot {
	protocol, ok := profile.ProviderProtocolSpecForSpec(string(profile.ProviderSpecRunPod), protocolName)
	if !ok {
		panic("runpod test target uses an unknown concrete protocol")
	}
	target := provider.NewTargetSnapshot("runpod", string(profile.ProviderSpecRunPod), baseURL, credential, kind, protocolName, protocol.Delivery)
	target.Model = "served-model"
	return target
}

func runpodOptionalRequest(t *testing.T) (canonical.CanonicalRequest, provider.AttemptToolNames) {
	t.Helper()
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	tool := canonicaltest.MustFunctionTool(key, "lookup", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	image, err := canonical.NewURLImage("https://example.test/image.png", canonical.Unspecified[canonical.ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	message, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart("describe this image"), canonical.NewImageMessagePart(image)})
	if err != nil {
		t.Fatal(err)
	}
	format, err := canonical.NewOutputFormat(canonical.OutputFormatParams{Kind: canonical.OutputFormatJSONObject})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:        canonical.Specify("served-model"),
		Items:        []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, tool), message},
		OutputFormat: canonical.Specify(format),
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	return request, names
}
