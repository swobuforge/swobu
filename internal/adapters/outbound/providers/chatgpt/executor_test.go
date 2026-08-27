package chatgpt

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	outboundcredentials "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

type stubCredentialResolver struct{}

func (stubCredentialResolver) ResolveCredential(ctx context.Context, providerSpec string, credentialRef string) (string, error) {
	return "token_test", nil
}

type failingCredentialResolver struct{}

func (failingCredentialResolver) ResolveCredential(ctx context.Context, providerSpec string, credentialRef string) (string, error) {
	return "", errors.New("boom")
}

type captureRoundTripper struct {
	lastRequest *http.Request
	lastBody    []byte
	statusCode  int
	body        string
}

func (c *captureRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	c.lastRequest = req.Clone(req.Context())
	if req.Body != nil {
		defer func() { _ = req.Body.Close() }()
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		c.lastBody = append(c.lastBody[:0], raw...)
	}
	body := c.body
	if body == "" {
		body = `{"id":"resp_1","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`
	}
	status := c.statusCode
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

type closeTrackingBody struct {
	io.Reader
	closed bool
}

func (b *closeTrackingBody) Close() error {
	b.closed = true
	return nil
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type recordingChanges = []compat.Change

type testProviderRequest struct {
	Request    canonical.CanonicalRequest
	Contract   exchange.ExecutionContract
	Target     provider.TargetSnapshot
	ExchangeID string
	Changes    *[]compat.Change
}

func newTestProviderRequest(exchangeID string, _ any, request canonical.CanonicalRequest, _ carrier.Document, contract exchange.ExecutionContract, target provider.TargetSnapshot, sinks ...*[]compat.Change) testProviderRequest {
	var sink *[]compat.Change
	if len(sinks) > 0 {
		sink = sinks[0]
	}
	return testProviderRequest{Request: request, Contract: contract, Target: target, ExchangeID: exchangeID, Changes: sink}
}

func executeTestProviderRequest(ctx context.Context, exec BackendAdapter, req testProviderRequest) (provider.Ingress, error) {
	target := req.Target.Clone()
	target.Model = req.Request.Model()
	backend, err := exec.ResolveBackend(target)
	if err != nil {
		return nil, err
	}
	names, _, err := provider.BuildAttemptToolNames(req.Request)
	if err != nil {
		return nil, err
	}
	doc, _, err := backend.Codec.Encode(provider.Request{Canonical: req.Request, ToolNames: names, Delivery: req.Contract.ProviderDelivery})
	if err != nil {
		return nil, err
	}
	return backend.Transport.Send(ctx, doc)
}

func mustJSONBodyMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return body
}

func TestListModels_LoadsBundledTierModels(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", "")
	claims := `{"https://api.openai.com/auth":{"chatgpt_plan_type":"plus"}}`
	idToken := "a." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".c"
	raw, err := outboundcredentials.EncodeTokenBundle(outboundcredentials.TokenBundle{
		AccessToken: "token-x",
		IDToken:     idToken,
		IssuedAt:    time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("encode bundle: %v", err)
	}
	if err := outboundcredentials.StoreSecretByRef("chatgpt", "secretfile:chatgpt/acct_plus", raw); err != nil {
		t.Fatalf("store secretfile bundle: %v", err)
	}
	exec := NewExecutor(http.DefaultClient, stubCredentialResolver{})
	models, err := exec.ListDeployments(context.Background(), provider.NewTargetSnapshot(
		"draft",
		"chatgpt",
		"https://chatgpt.com/backend-api/codex",
		"secretfile:chatgpt/acct_plus",
		protocolkind.ChatCompletions,
		"", delivery.BufferedDelivery()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("expected non-empty bundled models for plus tier")
	}
}

func TestListModels_DoesNotInferTierFromCredentialRefPathSegment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", "")

	exec := NewExecutor(http.DefaultClient, stubCredentialResolver{})
	models, err := exec.ListDeployments(context.Background(), provider.NewTargetSnapshot(
		"draft",
		"chatgpt",
		"https://chatgpt.com/backend-api/codex",
		"secretfile:chatgpt/plus/sess_abc",
		protocolkind.ChatCompletions,
		"", delivery.BufferedDelivery()))
	if err == nil {
		t.Fatalf("expected error, got models=%v", models)
	}
}

func TestResolveBackendResponsesNeedsNoResumptionCallback(t *testing.T) {
	target := provider.NewTargetSnapshot(
		"chatgpt-default",
		string(profile.ProviderSpecChatGPT),
		"",
		"secret:chatgpt/default",
		protocolkind.Responses,
		"responses_stream",
		delivery.StreamingDelivery(delivery.FramingSSE))
	target.Model = "gpt-test"
	backend, err := NewExecutor(nil, stubCredentialResolver{}).ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	if backend.Target.TargetID != target.TargetID || backend.Target.TargetVersion != target.TargetVersion {
		t.Fatalf("backend target = %#v, want exact target %#v", backend.Target, target)
	}
}

func TestListModels_UnknownTierReturnsError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", "")

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"model_ids":["gpt-5.5"]}`))
	}))
	defer srv.Close()
	claims := `{"https://api.openai.com/auth":{"chatgpt_plan_type":"enterprise"}}`
	idToken := "a." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".c"
	raw, err := outboundcredentials.EncodeTokenBundle(outboundcredentials.TokenBundle{
		AccessToken: "token-x",
		IDToken:     idToken,
		IssuedAt:    time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("encode bundle: %v", err)
	}
	if err := outboundcredentials.StoreSecretByRef("chatgpt", "secretfile:chatgpt/default", raw); err != nil {
		t.Fatalf("store secretfile bundle: %v", err)
	}

	exec := NewExecutor(srv.Client(), stubCredentialResolver{})
	models, err := exec.ListDeployments(context.Background(), provider.NewTargetSnapshot(
		"draft",
		"chatgpt",
		srv.URL+"/v1",
		"secretfile:chatgpt/default",
		protocolkind.ChatCompletions,
		"", delivery.BufferedDelivery()))
	if err == nil {
		t.Fatalf("expected error, got models=%v", models)
	}
	if called {
		t.Fatal("network must not be used when tier is unknown")
	}
}

func TestListModels_ResolvesTierFromStoredSecretBundleWhenRefHasNoTierSegment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", "")
	claims := `{"https://api.openai.com/auth":{"chatgpt_plan_type":"team"}}`
	idToken := "a." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".c"
	raw, err := outboundcredentials.EncodeTokenBundle(outboundcredentials.TokenBundle{
		AccessToken: "token-x",
		IDToken:     idToken,
		IssuedAt:    time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("encode bundle: %v", err)
	}
	if err := outboundcredentials.StoreSecretByRef("chatgpt", "secretfile:chatgpt/sess_abc", raw); err != nil {
		t.Fatalf("store secretfile bundle: %v", err)
	}

	exec := NewExecutor(http.DefaultClient, stubCredentialResolver{})
	models, err := exec.ListDeployments(context.Background(), provider.NewTargetSnapshot(
		"draft",
		"chatgpt",
		"https://chatgpt.com/backend-api/codex",
		"secretfile:chatgpt/sess_abc",
		protocolkind.ChatCompletions,
		"", delivery.BufferedDelivery()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("expected non-empty bundled models for team tier")
	}
}

func TestExecute_UsesChatGPTCodexEndpointForOpenAIBaseURL(t *testing.T) {
	t.Parallel()

	rt := &captureRoundTripper{}
	exec := NewExecutor(&http.Client{Transport: rt}, stubCredentialResolver{})
	req := newTestProviderRequest("test-ex", protocolkind.Responses,
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("gpt-5.4-mini"),
			Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
		}),
		carrier.Document{},
		exchange.NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
		provider.NewTargetSnapshot(
			"draft",
			string(profile.ProviderSpecChatGPT),
			"https://api.openai.com/v1",
			"secret:chatgpt/plus/sess_abc",
			protocolkind.Responses,
			"responses_stream", delivery.StreamingDelivery(delivery.FramingSSE)),
	)
	resp, err := executeTestProviderRequest(context.Background(), exec, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	streamIngress, ok := resp.(provider.StreamIngress)
	if !ok || streamIngress.Stream.Body == nil {
		t.Fatal("expected transport stream response")
	}
	streamBody := streamIngress.Stream
	if closeErr := streamBody.Body.Close(); closeErr != nil {
		t.Fatalf("close stream: %v", closeErr)
	}
	if rt.lastRequest == nil {
		t.Fatal("expected outbound request")
	}
	parsedBase, err := url.Parse(chatGPTCodexExecuteBase)
	if err != nil {
		t.Fatalf("parse codex base: %v", err)
	}
	if rt.lastRequest.URL.Host != parsedBase.Host {
		t.Fatalf("host=%q", rt.lastRequest.URL.Host)
	}
	if rt.lastRequest.URL.Path != parsedBase.Path+"/responses" {
		t.Fatalf("path=%q", rt.lastRequest.URL.Path)
	}
	if rt.lastRequest.Header.Get("Authorization") != "Bearer token_test" {
		t.Fatalf("authorization=%q", rt.lastRequest.Header.Get("Authorization"))
	}
	if rt.lastRequest.Header.Get(chatGPTSubagentHeaderKey) != chatGPTSubagentHeaderVal {
		t.Fatalf("subagent=%q", rt.lastRequest.Header.Get(chatGPTSubagentHeaderKey))
	}
}

func TestExecute_DoesNotEmitCacheCompatibilityDecisions(t *testing.T) {
	t.Parallel()

	rt := &captureRoundTripper{}
	exec := NewExecutor(&http.Client{Transport: rt}, stubCredentialResolver{})
	sink := &recordingChanges{}
	req := newTestProviderRequest("test-ex", protocolkind.Responses,
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("gpt-5.4-mini"),
			Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
		}),
		carrier.Document{},
		exchange.NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
		provider.NewTargetSnapshot(
			"draft",
			string(profile.ProviderSpecChatGPT),
			"https://api.openai.com/v1",
			"secret:chatgpt/plus/sess_abc",
			protocolkind.Responses,
			"responses_stream", delivery.StreamingDelivery(delivery.FramingSSE)),

		sink,
	)
	req.ExchangeID = "ex-chatgpt-cache"

	resp, err := executeTestProviderRequest(context.Background(), exec, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	streamIngress, ok := resp.(provider.StreamIngress)
	if !ok || streamIngress.Stream.Body == nil {
		t.Fatal("expected transport stream response")
	}
	streamBody := streamIngress.Stream
	if closeErr := streamBody.Body.Close(); closeErr != nil {
		t.Fatalf("close stream: %v", closeErr)
	}
	body := mustJSONBodyMap(t, rt.lastBody)
	if _, ok := body["prompt_cache_key"]; ok {
		t.Fatalf("prompt_cache_key must be omitted")
	}
	if _, ok := body["prompt_cache_retention"]; ok {
		t.Fatalf("prompt_cache_retention must be omitted")
	}
	if len(*sink) != 0 {
		t.Fatalf("compatibility changes len=%d want 0", len(*sink))
	}
}

func TestExecute_RejectsCurrentWebSearchDeclarationBeforeTransport(t *testing.T) {
	t.Parallel()

	rt := &captureRoundTripper{}
	exec := NewExecutor(&http.Client{Transport: rt}, stubCredentialResolver{})
	set, err := canonical.NewToolSet([]canonical.ToolDeclaration{canonical.NewWebSearchDeclaration()})
	if err != nil {
		t.Fatal(err)
	}
	req := newTestProviderRequest("test-ex", protocolkind.Responses,
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("gpt-5.4-mini"),
			Items: []canonical.CanonicalItem{
				canonicaltest.ToolDeclarations(t, set.Declarations()...),
				canonicaltest.Message(t, canonical.MessageRoleUser, "search news"),
			},
		}),
		carrier.Document{},
		exchange.NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
		provider.NewTargetSnapshot(
			"draft",
			string(profile.ProviderSpecChatGPT),
			"https://api.openai.com/v1",
			"secret:chatgpt/plus/sess_abc",
			protocolkind.Responses,
			"responses_stream", delivery.StreamingDelivery(delivery.FramingSSE)),
	)
	_, err = executeTestProviderRequest(context.Background(), exec, req)
	var incompatible provider.IncompatibleTargetError
	if !errors.As(err, &incompatible) {
		t.Fatalf("error = %T, want IncompatibleTargetError", err)
	}
	if len(rt.lastBody) != 0 {
		t.Fatalf("current hosted search reached transport: %s", rt.lastBody)
	}
}

func TestExecute_UsesProvidedCodexBaseURL(t *testing.T) {
	t.Parallel()

	var seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	exec := NewExecutor(srv.Client(), stubCredentialResolver{})
	req := newTestProviderRequest("test-ex", protocolkind.Responses,
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("gpt-5.4-mini"),
			Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
		}),
		carrier.Document{},
		exchange.NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
		provider.NewTargetSnapshot(
			"draft",
			string(profile.ProviderSpecChatGPT),
			srv.URL+"/backend-api/codex",
			"secret:chatgpt/plus/sess_abc",
			protocolkind.Responses,
			"responses_stream", delivery.StreamingDelivery(delivery.FramingSSE)),
	)
	if _, err := executeTestProviderRequest(context.Background(), exec, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seenPath != "/backend-api/codex/responses" {
		t.Fatalf("path=%q", seenPath)
	}
}

func TestSend_NonSSEStreamingSuccessReturnsBoundedBackendFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
	}{
		{name: "json", contentType: "application/json"},
		{name: "malformed", contentType: `text/event-stream; charset="`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := &closeTrackingBody{Reader: strings.NewReader(strings.Repeat("x", (64<<10)+4096))}
			client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				header := make(http.Header)
				if tt.contentType != "" {
					header.Set("Content-Type", tt.contentType)
				}
				header.Set("Retry-After", "7")
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     header,
					Body:       body,
					Request:    req,
				}, nil
			})}
			target := provider.NewTargetSnapshot(
				"chatgpt-target",
				string(profile.ProviderSpecChatGPT),
				"https://chatgpt.com/backend-api/codex",
				"secretfile:chatgpt/test",
				protocolkind.Responses,
				"responses_stream",
				delivery.StreamingDelivery(delivery.FramingSSE))
			doc := carrier.NewDocument(
				protocolkind.Responses,
				"application/json",
				nil,
				[]byte(`{"model":"gpt-5.4-mini","input":[],"stream":true}`),
				carrier.Meta{},
			)

			_, err := NewExecutor(client, stubCredentialResolver{}).Send(context.Background(), target, doc)
			var backendErr canonical.BackendError
			if !errors.As(err, &backendErr) {
				t.Fatalf("error = %T, want canonical.BackendError", err)
			}
			if backendErr.StatusCode != http.StatusBadGateway {
				t.Fatalf("status = %d, want %d", backendErr.StatusCode, http.StatusBadGateway)
			}
			if backendErr.Origin != canonical.ErrorOriginBackend {
				t.Fatalf("origin = %q, want %q", backendErr.Origin, canonical.ErrorOriginBackend)
			}
			if backendErr.RetryAfterHeaderValue != "7" {
				t.Fatalf("retry-after = %q, want 7", backendErr.RetryAfterHeaderValue)
			}
			if len(backendErr.Message) > 64<<10 {
				t.Fatalf("backend evidence length = %d, want <= %d", len(backendErr.Message), 64<<10)
			}
			if !body.closed {
				t.Fatal("non-SSE response body was not closed")
			}
		})
	}
}

func TestSend_MissingContentTypeNormalizesChatGPTStreamCarrier(t *testing.T) {
	t.Parallel()

	const sse = "event: response.output_text.delta\ndata: {\"delta\":\"OK\"}\n\n"
	client := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(sse)),
			Request:    req,
		}, nil
	})}
	target := provider.NewTargetSnapshot(
		"chatgpt-target",
		string(profile.ProviderSpecChatGPT),
		"https://chatgpt.com/backend-api/codex",
		"secretfile:chatgpt/test",
		protocolkind.Responses,
		"responses_stream",
		delivery.StreamingDelivery(delivery.FramingSSE))
	doc := carrier.NewDocument(
		protocolkind.Responses,
		"application/json",
		nil,
		[]byte(`{"model":"gpt-5.4-mini","input":[],"stream":true}`),
		carrier.Meta{},
	)

	ingress, err := NewExecutor(client, stubCredentialResolver{}).Send(context.Background(), target, doc)
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	streamIngress, ok := ingress.(provider.StreamIngress)
	if !ok {
		t.Fatalf("ingress = %T, want provider.StreamIngress", ingress)
	}
	if streamIngress.Stream.MediaType != "text/event-stream" {
		t.Fatalf("media type = %q, want text/event-stream", streamIngress.Stream.MediaType)
	}
	raw, err := io.ReadAll(streamIngress.Stream.Body)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	_ = streamIngress.Stream.Body.Close()
	if string(raw) != sse {
		t.Fatalf("stream body = %q, want %q", raw, sse)
	}
}

func TestExecute_CredentialResolutionFailureReturnsBadEndpoint(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("request must not be sent when credential resolution fails")
	}))
	defer srv.Close()

	exec := NewExecutor(srv.Client(), failingCredentialResolver{})
	req := newTestProviderRequest("test-ex", protocolkind.Responses,
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("gpt-5.4-mini"),
			Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
		}),
		carrier.Document{},
		exchange.NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
		provider.NewTargetSnapshot(
			"draft",
			string(profile.ProviderSpecChatGPT),
			srv.URL+"/backend-api/codex",
			"secret:chatgpt/plus/sess_abc",
			protocolkind.Responses,
			"responses_stream", delivery.StreamingDelivery(delivery.FramingSSE)),
	)
	_, err := executeTestProviderRequest(context.Background(), exec, req)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "credential reference could not be resolved") {
		t.Fatalf("error=%v", err)
	}
}

func TestExecute_UnauthorizedRefreshesBundleAndRetriesOnce(t *testing.T) {
	t.Setenv("SWOBU_HOME", t.TempDir()+"/swobu-home")

	origRefreshURL := chatGPTRefreshTokenURL
	t.Cleanup(func() { chatGPTRefreshTokenURL = origRefreshURL })

	refreshSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"token_fresh","refresh_token":"refresh_next","expires_in":3600}`))
	}))
	defer refreshSrv.Close()
	chatGPTRefreshTokenURL = refreshSrv.URL

	bundle, err := outboundcredentials.EncodeTokenBundle(outboundcredentials.TokenBundle{
		AccessToken:  "token_old",
		RefreshToken: "refresh_1",
		ExpiresAt:    time.Now().UTC().Add(-1 * time.Minute),
		IssuedAt:     time.Now().UTC().Add(-2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("encode bundle: %v", err)
	}
	ref, err := outboundcredentials.StoreMaterializedCredential("chatgpt", "chatgpt/plus/sess_abc", bundle, outboundcredentials.CredentialWritePolicyFile)
	if err != nil {
		t.Fatalf("store credential: %v", err)
	}

	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		auth := strings.TrimSpace(r.Header.Get("Authorization")) // swobu:io-string source=domain
		if attempts == 1 {
			if auth != "Bearer token_old" {
				t.Fatalf("first auth=%q", auth)
			}
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"expired"}`))
			return
		}
		if auth != "Bearer token_fresh" {
			t.Fatalf("second auth=%q", auth)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	exec := NewExecutor(srv.Client(), outboundcredentials.NewResolver())
	req := newTestProviderRequest("test-ex", protocolkind.Responses,
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("gpt-5.4-mini"),
			Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
		}),
		carrier.Document{},
		exchange.NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
		provider.NewTargetSnapshot(
			"draft",
			string(profile.ProviderSpecChatGPT),
			srv.URL+"/backend-api/codex",
			ref,
			protocolkind.Responses,
			"responses_stream", delivery.StreamingDelivery(delivery.FramingSSE)),
	)
	resp, err := executeTestProviderRequest(context.Background(), exec, req)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if streamIngress, ok := resp.(provider.StreamIngress); ok && streamIngress.Stream.Body != nil {
		_ = streamIngress.Stream.Body.Close()
	}
	if attempts != 2 {
		t.Fatalf("attempts=%d want 2", attempts)
	}

	raw, err := outboundcredentials.ResolveStoredSecretByRef("chatgpt", ref)
	if err != nil {
		t.Fatalf("resolve stored secret: %v", err)
	}
	var persisted map[string]any
	if err := json.Unmarshal([]byte(raw), &persisted); err != nil {
		t.Fatalf("decode persisted secret: %v", err)
	}
	if strings.TrimSpace(persisted["access_token"].(string)) != "token_fresh" { // swobu:io-string source=domain
		t.Fatalf("persisted access token=%v", persisted["access_token"])
	}
}

func TestExecute_StreamingReturnsTransportStream(t *testing.T) {
	t.Parallel()

	sse := "event: response.output_text.delta\ndata: {\"delta\":\"hello\"}\n\n" +
		"event: response.completed\ndata: {\"response\":{\"id\":\"resp_1\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello\"}]}]}}\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sse)
	}))
	defer srv.Close()

	exec := NewExecutor(srv.Client(), stubCredentialResolver{})
	req := newTestProviderRequest("test-ex", protocolkind.Responses,
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify("gpt-5.4-mini"),
			Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
		}),
		carrier.Document{},
		exchange.NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
		provider.NewTargetSnapshot(
			"draft",
			string(profile.ProviderSpecChatGPT),
			srv.URL+"/backend-api/codex",
			"secret:chatgpt/plus/sess_abc",
			protocolkind.Responses,
			"responses_stream", delivery.StreamingDelivery(delivery.FramingSSE)),
	)
	resp, err := executeTestProviderRequest(context.Background(), exec, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	streamIngress, ok := resp.(provider.StreamIngress)
	if !ok || streamIngress.Stream.Body == nil {
		t.Fatal("expected transport stream response")
	}
	streamBody := streamIngress.Stream
	if closeErr := streamBody.Body.Close(); closeErr != nil {
		t.Fatalf("close stream: %v", closeErr)
	}
}
