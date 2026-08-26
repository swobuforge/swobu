package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestTransportPostsNativeInteractionsWithGoogleAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/interactions" {
			t.Fatalf("method/path = %s %s", request.Method, request.URL.Path)
		}
		if got := request.Header.Get("x-goog-api-key"); got != "gemini-token" {
			t.Fatalf("x-goog-api-key = %q", got)
		}
		if got := request.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization = %q, want absent", got)
		}
		if got := request.Header.Get("Accept"); got != "text/event-stream" {
			t.Fatalf("Accept = %q", got)
		}
		if got := request.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["model"] != "gemini-model" || payload["stream"] != true {
			t.Fatalf("request payload = %#v", payload)
		}
		writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		_, _ = io.WriteString(writer, "data: {\"event_type\":\"error\",\"error\":{\"code\":\"upstream\",\"message\":\"stop\"}}\n\n")
	}))
	defer server.Close()

	backend := geminiBackend(t, server.Client(), server.URL+"/v1")
	document := geminiTextDocument(t, backend)
	ingress, err := backend.Transport.Send(context.Background(), document)
	if err != nil {
		t.Fatal(err)
	}
	stream, ok := ingress.(provider.StreamIngress)
	if !ok || stream.Stream.MediaType != "text/event-stream; charset=utf-8" || stream.Stream.Body == nil {
		t.Fatalf("ingress = %#v, want SSE stream", ingress)
	}
	_ = stream.Stream.Body.Close()
}

func TestTransportKeepsBackendFailuresBackendOriginated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Retry-After", "7")
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(writer, `{"error":"quota"}`)
	}))
	defer server.Close()

	backend := geminiBackend(t, server.Client(), server.URL+"/v1")
	_, err := backend.Transport.Send(context.Background(), geminiTextDocument(t, backend))
	var failure provider.AttemptFailure
	if !errors.As(err, &failure) || failure.Execution() != provider.ExecutionMayHaveOccurred {
		t.Fatalf("failure = %T %v, want may-have-executed attempt failure", err, err)
	}
	var backendError canonical.BackendError
	if !errors.As(err, &backendError) || backendError.StatusCode != http.StatusTooManyRequests || backendError.RetryAfterHeaderValue != "7" {
		t.Fatalf("backend error = %#v", backendError)
	}
}

func TestTransportRejectsSuccessfulNonSSEResponseAsBackendFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"not":"an event stream"}`)
	}))
	defer server.Close()

	backend := geminiBackend(t, server.Client(), server.URL+"/v1")
	_, err := backend.Transport.Send(context.Background(), geminiTextDocument(t, backend))
	var failure provider.AttemptFailure
	if !errors.As(err, &failure) || failure.Execution() != provider.ExecutionMayHaveOccurred {
		t.Fatalf("failure = %T %v, want may-have-executed failure", err, err)
	}
	var backendError canonical.BackendError
	if !errors.As(err, &backendError) || backendError.StatusCode != http.StatusBadGateway || !strings.Contains(backendError.Message, "not") {
		t.Fatalf("backend error = %#v", backendError)
	}
}

func TestDiscoveryPaginatesNativeModelsAndUsesBaseModelID(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		paths = append(paths, request.URL.RequestURI())
		if request.Method != http.MethodGet || request.URL.Path != "/v1beta/models" {
			t.Fatalf("method/path = %s %s", request.Method, request.URL.Path)
		}
		if got := request.Header.Get("x-goog-api-key"); got != "gemini-token" {
			t.Fatalf("x-goog-api-key = %q", got)
		}
		if request.Header.Get("Authorization") != "" {
			t.Fatal("Bearer authentication must not be sent to Gemini")
		}
		if request.URL.Query().Get("pageToken") == "page-two" {
			_, _ = io.WriteString(writer, `{"models":[{"baseModelId":"gemini-2","displayName":"Second"},{"baseModelId":"","displayName":"Ignored"}]}`)
			return
		}
		_, _ = io.WriteString(writer, `{"models":[{"baseModelId":"gemini-1","displayName":"First"},{"name":"publishers/google/models/not-direct","displayName":"Wrong scope"}],"nextPageToken":"page-two"}`)
	}))
	defer server.Close()

	target := geminiTarget()
	target.BaseURL = server.URL + "/v1"
	result, err := NewRuntime(server.Client(), credentialResolver{}).Discovery.ProbeTarget(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(paths, ","); got != "/v1beta/models,/v1beta/models?pageToken=page-two" {
		t.Fatalf("requests = %s", got)
	}
	if len(result.Options) != 2 || result.Options[0].Name != "gemini-1" || result.Options[0].ModelName != "gemini-1" || result.Options[1].Name != "gemini-2" || result.Options[1].ModelName != "gemini-2" {
		t.Fatalf("deployments = %#v", result.Options)
	}
}

func TestDiscoveryUsesExactModelsResourceNameWhenBaseModelIDIsAbsent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"models":[{"name":"models/gemini-current","displayName":"Current"},{"name":"publishers/google/models/not-a-direct-model","displayName":"Wrong scope"},{"name":"models/nested/id","displayName":"Nested"}]}`)
	}))
	defer server.Close()

	target := geminiTarget()
	target.BaseURL = server.URL + "/v1"
	result, err := NewRuntime(server.Client(), credentialResolver{}).Discovery.ProbeTarget(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Options) != 1 || result.Options[0].Name != "gemini-current" || result.Options[0].ModelName != "gemini-current" {
		t.Fatalf("deployments = %#v", result.Options)
	}
}

func TestADCHeadersReachDiscoveryAndInferenceWithoutAPIKeyFallback(t *testing.T) {
	for _, operation := range []string{"discovery", "inference"} {
		t.Run(operation, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requests++
				if got := request.Header.Get("Authorization"); got != "Bearer adc-token" {
					t.Fatalf("Authorization = %q", got)
				}
				if got := request.Header.Get("x-goog-user-project"); got != "quota-project" {
					t.Fatalf("x-goog-user-project = %q", got)
				}
				if got := request.Header.Get("x-goog-api-key"); got != "" {
					t.Fatalf("x-goog-api-key = %q, want absent", got)
				}
				writer.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(writer, `{"error":{"message":"denied"}}`)
			}))
			defer server.Close()

			resolver := &countingCredentialResolver{}
			ambient := &fakeADC{tokens: []string{"adc-token"}, quotaProject: "quota-project"}
			bundle := newRuntime(server.Client(), resolver, func(context.Context) (adcCredentials, error) { return ambient, nil })
			target := geminiTarget()
			target.BaseURL, target.CredentialRef = server.URL+"/v1", ""
			if operation == "discovery" {
				_, _ = bundle.Discovery.ProbeTarget(context.Background(), target)
			} else {
				backend, err := bundle.BackendResolver.ResolveBackend(target)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = backend.Transport.Send(context.Background(), geminiTextDocument(t, backend))
			}
			if requests != 1 || resolver.calls != 0 || ambient.tokenCalls != 1 {
				t.Fatalf("calls after backend rejection: requests=%d resolver=%d tokens=%d", requests, resolver.calls, ambient.tokenCalls)
			}
		})
	}
}

func geminiBackend(t *testing.T, client *http.Client, baseURL string) provider.Backend {
	t.Helper()
	target := geminiTarget()
	target.BaseURL = baseURL
	backend, err := NewRuntime(client, credentialResolver{}).BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	return backend
}

func geminiTextDocument(t *testing.T, backend provider.Backend) carrier.Document {
	t.Helper()
	document, _, err := backend.Codec.Encode(provider.Request{
		Canonical: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("gemini-model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
		}),
		Delivery: delivery.StreamingDelivery(delivery.FramingSSE),
	})
	if err != nil {
		t.Fatal(err)
	}
	return document
}
