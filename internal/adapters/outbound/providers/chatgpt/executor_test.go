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
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/ports"
	"github.com/swobuforge/swobu/internal/profile"
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
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

type recordingEffectSink struct {
	effects []effect.Effect
}

func (s *recordingEffectSink) Commit(_ context.Context, _ string, effects []effect.Effect) error {
	s.effects = append(s.effects, effects...)
	return nil
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
	models, err := exec.ListDeployments(context.Background(), exchange.NewRoutableTarget(
		"draft",
		"chatgpt",
		"https://chatgpt.com/backend-api/codex",
		"secretfile:chatgpt/acct_plus",
		protocolkind.ChatCompletions,
		"credential_ref",
		"",
		"",
	))
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
	models, err := exec.ListDeployments(context.Background(), exchange.NewRoutableTarget(
		"draft",
		"chatgpt",
		"https://chatgpt.com/backend-api/codex",
		"secretfile:chatgpt/plus/sess_abc",
		protocolkind.ChatCompletions,
		"credential_ref",
		"",
		"",
	))
	if err == nil {
		t.Fatalf("expected error, got models=%v", models)
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
	models, err := exec.ListDeployments(context.Background(), exchange.NewRoutableTarget(
		"draft",
		"chatgpt",
		srv.URL+"/v1",
		"secretfile:chatgpt/default",
		protocolkind.ChatCompletions,
		"credential_ref",
		"",
		"",
	))
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
	models, err := exec.ListDeployments(context.Background(), exchange.NewRoutableTarget(
		"draft",
		"chatgpt",
		"https://chatgpt.com/backend-api/codex",
		"secretfile:chatgpt/sess_abc",
		protocolkind.ChatCompletions,
		"credential_ref",
		"",
		"",
	))
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
	req := ports.NewProviderRequest(
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "gpt-5.4-mini",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hello")},
		}),
		carrier.WireDocument{},
		exchange.NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
		exchange.NewRoutableTarget(
			"draft",
			string(profile.ProviderSpecChatGPT),
			"https://api.openai.com/v1",
			"keychain:chatgpt/plus/sess_abc",
			protocolkind.Responses,
			"backend_chatgpt",
			"", "responses_stream",
		),
	)
	resp, err := exec.ResolveProviderIngress(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	streamBody, ok := resp.(carrier.WireStream)
	if !ok || streamBody.Frames == nil {
		t.Fatal("expected transport stream response")
	}
	if closeErr := streamBody.Frames.Close(); closeErr != nil {
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
	sink := &recordingEffectSink{}
	req := ports.NewProviderRequest(
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "gpt-5.4-mini",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hello")},
		}),
		carrier.WireDocument{},
		exchange.NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
		exchange.NewRoutableTarget(
			"draft",
			string(profile.ProviderSpecChatGPT),
			"https://api.openai.com/v1",
			"keychain:chatgpt/plus/sess_abc",
			protocolkind.Responses,
			"backend_chatgpt",
			"", "responses_stream",
		),
		sink,
	)
	req.ExchangeID = "ex-chatgpt-cache"

	resp, err := exec.ResolveProviderIngress(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	streamBody, ok := resp.(carrier.WireStream)
	if !ok || streamBody.Frames == nil {
		t.Fatal("expected transport stream response")
	}
	if closeErr := streamBody.Frames.Close(); closeErr != nil {
		t.Fatalf("close stream: %v", closeErr)
	}
	body := mustJSONBodyMap(t, rt.lastBody)
	if _, ok := body["prompt_cache_key"]; ok {
		t.Fatalf("prompt_cache_key must be omitted")
	}
	if _, ok := body["prompt_cache_retention"]; ok {
		t.Fatalf("prompt_cache_retention must be omitted")
	}
	if len(sink.effects) != 0 {
		t.Fatalf("compatibility effects len=%d want 0", len(sink.effects))
	}
}

func TestExecute_UsesProvidedCodexBaseURL(t *testing.T) {
	t.Parallel()

	var seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer srv.Close()

	exec := NewExecutor(srv.Client(), stubCredentialResolver{})
	req := ports.NewProviderRequest(
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "gpt-5.4-mini",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hello")},
		}),
		carrier.WireDocument{},
		exchange.NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
		exchange.NewRoutableTarget(
			"draft",
			string(profile.ProviderSpecChatGPT),
			srv.URL+"/backend-api/codex",
			"keychain:chatgpt/plus/sess_abc",
			protocolkind.Responses,
			"backend_chatgpt",
			"", "responses_stream",
		),
	)
	if _, err := exec.ResolveProviderIngress(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seenPath != "/backend-api/codex/responses" {
		t.Fatalf("path=%q", seenPath)
	}
}

func TestExecute_CredentialResolutionFailureReturnsBadEndpoint(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("request must not be sent when credential resolution fails")
	}))
	defer srv.Close()

	exec := NewExecutor(srv.Client(), failingCredentialResolver{})
	req := ports.NewProviderRequest(
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "gpt-5.4-mini",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hello")},
		}),
		carrier.WireDocument{},
		exchange.NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
		exchange.NewRoutableTarget(
			"draft",
			string(profile.ProviderSpecChatGPT),
			srv.URL+"/backend-api/codex",
			"keychain:chatgpt/plus/sess_abc",
			protocolkind.Responses,
			"backend_chatgpt",
			"", "responses_stream",
		),
	)
	_, err := exec.ResolveProviderIngress(context.Background(), req)
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
		_, _ = w.Write([]byte(`{"id":"resp_1","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`))
	}))
	defer srv.Close()

	exec := NewExecutor(srv.Client(), outboundcredentials.NewResolver())
	req := ports.NewProviderRequest(
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "gpt-5.4-mini",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hello")},
		}),
		carrier.WireDocument{},
		exchange.NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
		exchange.NewRoutableTarget(
			"draft",
			string(profile.ProviderSpecChatGPT),
			srv.URL+"/backend-api/codex",
			ref,
			protocolkind.Responses,
			"backend_chatgpt",
			"", "responses_stream",
		),
	)
	resp, err := exec.ResolveProviderIngress(context.Background(), req)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if streamBody, ok := resp.(carrier.WireStream); ok && streamBody.Frames != nil {
		_ = streamBody.Frames.Close()
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
	req := ports.NewProviderRequest(
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "gpt-5.4-mini",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hello")},
		}),
		carrier.WireDocument{},
		exchange.NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
		exchange.NewRoutableTarget(
			"draft",
			string(profile.ProviderSpecChatGPT),
			srv.URL+"/backend-api/codex",
			"keychain:chatgpt/plus/sess_abc",
			protocolkind.Responses,
			"backend_chatgpt",
			"", "responses_stream",
		),
	)
	resp, err := exec.ResolveProviderIngress(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	streamBody, ok := resp.(carrier.WireStream)
	if !ok || streamBody.Frames == nil {
		t.Fatal("expected transport stream response")
	}
	if closeErr := streamBody.Frames.Close(); closeErr != nil {
		t.Fatalf("close stream: %v", closeErr)
	}
}
