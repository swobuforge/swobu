package openaifamily

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestExactBackendOwnsChatCompletionsTokenFieldSpelling(t *testing.T) {
	maxTokens := 64
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{MaxOutputTokens: &maxTokens})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")}, Controls: controls,
	})

	for _, tc := range []struct {
		name       string
		providerID profile.ProviderID
		policy     ProviderRoutePolicy
		want       string
	}{
		{name: "official_openai", providerID: profile.ProviderSpecOpenAI, policy: NewOpenAIPolicy(), want: "max_completion_tokens"},
		{name: "openrouter", providerID: profile.ProviderSpecOpenRouter, policy: NewOpenRouterPolicy(), want: "max_tokens"},
		{name: "ollama", providerID: profile.ProviderSpecOllama, policy: NewOllamaPolicy(), want: "max_tokens"},
		{name: "custom", providerID: profile.ProviderSpecCustom, policy: NewCustomPolicy(), want: "max_tokens"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target := provider.NewTargetSnapshot("backend", string(tc.providerID), "https://example.test/v1", "env:TOKEN", protocolkind.ChatCompletions, "", "chat_completions")
			target.Model = request.Model()
			backend, err := NewExecutor(nil, nil, tc.policy).ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if payload[tc.want] != float64(maxTokens) {
				t.Fatalf("%s = %#v, want %d", tc.want, payload[tc.want], maxTokens)
			}
			other := "max_tokens"
			if tc.want == other {
				other = "max_completion_tokens"
			}
			if _, exists := payload[other]; exists {
				t.Fatalf("unexpected %s in %s", other, document.RawBytes())
			}
		})
	}
}

type failingRoundTripper struct {
	err error
}

type closeTrackingBody struct {
	io.Reader
	closed bool
}

func (b *closeTrackingBody) Close() error { b.closed = true; return nil }

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func (t failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, t.err
}

func TestSendProviderRequest_PreservesTransportErrorDetail(t *testing.T) {
	t.Parallel()

	transportErr := errors.New("dial tcp 127.0.0.1:11434: connect: connection refused")
	exec := NewExecutor(&http.Client{Transport: failingRoundTripper{err: transportErr}}, nil, NewOllamaPolicy())
	doc := carrier.NewDocument(
		protocolkind.Responses,
		"application/json",
		nil,
		[]byte(`{"model":"gpt-4o-mini","input":"hello"}`),
		carrier.Meta{},
	)
	target := provider.NewTargetSnapshot(
		"backend-a",
		string(profile.ProviderSpecOllama),
		"http://127.0.0.1:11434/v1",
		"",
		protocolkind.Responses,
		"",
		"",
	)
	target.Model = "gpt-4o-mini"

	_, err := exec.Send(context.Background(), target, doc)
	if err == nil {
		t.Fatal("expected SendProviderRequest to fail")
	}
	var unavailable provider.UnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("error type = %T, want provider.UnavailableError", err)
	}

	var swErr canonical.Error
	if !errors.As(err, &swErr) {
		t.Fatalf("error type = %T, want canonical.Error", err)
	}
	if swErr.Code != canonical.ErrorCodeBadEndpoint {
		t.Fatalf("error code = %s, want %s", swErr.Code, canonical.ErrorCodeBadEndpoint)
	}
	if got := swErr.Details["request_transport_error"]; got != transportErr.Error() {
		t.Fatalf("transport detail = %q, want %q", got, transportErr.Error())
	}
}

func TestSendProviderRequest_MarksConfirmedUnsupportedResponse(t *testing.T) {
	body := io.NopCloser(strings.NewReader(`{"error":{"message":"tool_choice required is unsupported","param":"tool_choice","code":"unsupported_parameter"}}`))
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: body, Request: req}, nil
	})}
	exec := NewExecutor(client, nil, NewOllamaPolicy())
	target := provider.NewTargetSnapshot("backend-a", string(profile.ProviderSpecOllama), "http://127.0.0.1:11434/v1", "", protocolkind.Responses, "", "")
	target.Model = "gpt-4o-mini"
	doc := carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(`{"model":"gpt-4o-mini","tool_choice":"required"}`), carrier.Meta{})

	_, err := exec.Send(context.Background(), target, doc)
	var unsupported provider.UnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("error = %T, want provider.UnsupportedError", err)
	}
}

func TestSendProviderRequest_BoundsNonSSEStreamingEvidenceAndClosesBody(t *testing.T) {
	body := &closeTrackingBody{Reader: strings.NewReader(strings.Repeat("x", (64<<10)+4096))}
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: body, Request: req}, nil
	})}
	exec := NewExecutor(client, nil, NewOllamaPolicy())
	target := provider.NewTargetSnapshot("backend-a", string(profile.ProviderSpecOllama), "http://127.0.0.1:11434/v1", "", protocolkind.Responses, "", "")
	target.Model = "gpt-4o-mini"
	doc := carrier.NewDocument(protocolkind.Responses, "application/json", nil, []byte(`{"model":"gpt-4o-mini","input":"hello","stream":true}`), carrier.Meta{})
	_, err := exec.Send(context.Background(), target, doc)
	var backendErr canonical.BackendError
	if !errors.As(err, &backendErr) || backendErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("error = %#v, want bounded 502 backend error", err)
	}
	if len(backendErr.Message) > 64<<10 {
		t.Fatalf("backend evidence length = %d, want <= %d", len(backendErr.Message), 64<<10)
	}
	if !body.closed {
		t.Fatal("non-SSE response body was not closed")
	}
}
