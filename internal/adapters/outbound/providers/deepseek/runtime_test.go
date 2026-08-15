package deepseek

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

type staticCredentialProvider struct{ token string }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type closeTrackingBody struct {
	io.Reader
	closed bool
}

func (b *closeTrackingBody) Close() error {
	b.closed = true
	return nil
}

func (p staticCredentialProvider) ResolveCredential(context.Context, string, string) (string, error) {
	return p.token, nil
}

func TestRuntimeUsesSharedMessagesTransport(t *testing.T) {
	var path, apiKey, version, body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		apiKey = r.Header.Get("x-api-key")
		version = r.Header.Get("anthropic-version")
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	runtime := NewRuntime(server.Client(), staticCredentialProvider{token: "deepseek-token"})
	target := provider.NewTargetSnapshot(
		"deepseek-pro",
		string(profile.ProviderSpecDeepSeek),
		server.URL+"/anthropic/v1",
		"env:DEEPSEEK_API_KEY",
		protocolkind.Messages,
		"messages_stream",
		delivery.StreamingDelivery(delivery.FramingSSE))
	target.Model = "deepseek-v4-pro"
	backend, err := runtime.BackendResolver.ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	codec, ok := backend.Codec.(protocolcodec.Codec)
	if !ok || codec.Protocol != protocolkind.Messages {
		t.Fatalf("codec = %#v, want shared Messages codec", backend.Codec)
	}
	ingress, err := backend.Transport.Send(context.Background(), carrier.NewDocument(
		protocolkind.Messages,
		"application/json",
		nil,
		[]byte(`{"model":"deepseek-v4-pro","stream":true}`),
		carrier.Meta{},
	))
	if err != nil {
		t.Fatal(err)
	}
	stream, ok := ingress.(provider.StreamIngress)
	if !ok {
		t.Fatalf("ingress = %T", ingress)
	}
	_ = stream.Stream.Body.Close()
	if path != "/anthropic/v1/messages" || apiKey != "deepseek-token" || version == "" {
		t.Fatalf("request path/auth/version = %q/%q/%q", path, apiKey, version)
	}
	if !strings.Contains(body, `"stream":true`) {
		t.Fatalf("request body = %s", body)
	}
}

func TestDiscoveryUsesRootModelsBearerAndPreservesIDs(t *testing.T) {
	var path, authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		authorization = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[{"id":"future-model"},{"id":"deepseek-v4-pro"}]}`))
	}))
	defer server.Close()
	discovery := Discovery{
		client:      server.Client(),
		credentials: staticCredentialProvider{token: "deepseek-token"},
		catalogURL:  server.URL + "/models",
	}
	target := provider.NewTargetSnapshot("deepseek-pro", "deepseek", "ignored", "env:DEEPSEEK_API_KEY", protocolkind.Messages, "messages_stream", delivery.StreamingDelivery(delivery.FramingSSE))
	deployments, err := discovery.ListDeployments(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if path != "/models" || authorization != "Bearer deepseek-token" {
		t.Fatalf("catalog path/auth = %q/%q", path, authorization)
	}
	if len(deployments) != 2 || deployments[0].Name != "deepseek-v4-pro" || deployments[1].Name != "future-model" {
		t.Fatalf("deployments = %#v", deployments)
	}
	for _, deployment := range deployments {
		if len(deployment.SupportedProviderProtocols) != 1 || deployment.SupportedProviderProtocols[0] != "messages_stream" {
			t.Fatalf("deployment protocols = %#v", deployment)
		}
	}
}

func TestDiscoveryAcceptsSuccessfulEmptyCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()
	discovery := Discovery{client: server.Client(), credentials: staticCredentialProvider{token: "token"}, catalogURL: server.URL + "/models"}
	target := provider.NewTargetSnapshot("deepseek-pro", "deepseek", "ignored", "env:DEEPSEEK_API_KEY", protocolkind.Messages, "messages_stream", delivery.StreamingDelivery(delivery.FramingSSE))
	deployments, err := discovery.ListDeployments(context.Background(), target)
	if err != nil || len(deployments) != 0 {
		t.Fatalf("deployments = %#v, error = %v", deployments, err)
	}
}

func TestDiscoveryReturnsUnsupportedContentEncodingWithoutPanic(t *testing.T) {
	body := &closeTrackingBody{Reader: strings.NewReader(`{"data":[]}`)}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Encoding": {"unsupported"}},
			Body:       body,
			Request:    request,
		}, nil
	})}
	discovery := Discovery{client: client, credentials: staticCredentialProvider{token: "token"}, catalogURL: "https://api.deepseek.test/models"}
	target := provider.NewTargetSnapshot("deepseek-pro", "deepseek", "ignored", "env:DEEPSEEK_API_KEY", protocolkind.Messages, "messages_stream", delivery.StreamingDelivery(delivery.FramingSSE))
	if _, err := discovery.ListDeployments(context.Background(), target); err == nil {
		t.Fatal("discovery unexpectedly accepted unsupported content encoding")
	}
	if !body.closed {
		t.Fatal("unsupported content-encoding response body was not closed")
	}
}
