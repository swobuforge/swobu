package chatgpt

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/ports"
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
	statusCode  int
	body        string
}

func (c *captureRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	c.lastRequest = req.Clone(req.Context())
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

func TestListModels_UsesTierCatalogEndpoint(t *testing.T) {
	t.Parallel()

	var seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		_, _ = w.Write([]byte(`{"model_ids":["gpt-5.5","gpt-5.4"]}`))
	}))
	defer srv.Close()

	exec := NewExecutor(srv.Client(), stubCredentialResolver{})
	exec.catalogBase = srv.URL
	models, err := exec.ListModels(context.Background(), ports.NewRoutableTarget(
		"draft",
		"chatgpt",
		srv.URL+"/v1",
		"keychain:chatgpt/acct_plus",
		protocolkind.ChatCompletions,
		"credential_ref",
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seenPath != "/api/v1/model-catalog/chatgpt/subscriptions/plus" {
		t.Fatalf("path=%q", seenPath)
	}
	if strings.Join(models, ",") != "gpt-5.4,gpt-5.5" {
		t.Fatalf("models=%v", models)
	}
}

func TestListModels_UsesTierFromCredentialRefPathSegment(t *testing.T) {
	t.Parallel()

	var seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		_, _ = w.Write([]byte(`{"model_ids":["gpt-5.5"]}`))
	}))
	defer srv.Close()

	exec := NewExecutor(srv.Client(), stubCredentialResolver{})
	exec.catalogBase = srv.URL
	_, err := exec.ListModels(context.Background(), ports.NewRoutableTarget(
		"draft",
		"chatgpt",
		srv.URL+"/v1",
		"keychain:chatgpt/plus/sess_abc",
		protocolkind.ChatCompletions,
		"credential_ref",
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if seenPath != "/api/v1/model-catalog/chatgpt/subscriptions/plus" {
		t.Fatalf("path=%q", seenPath)
	}
}

func TestListModels_UnknownTierReturnsError(t *testing.T) {
	t.Parallel()

	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"model_ids":["gpt-5.5"]}`))
	}))
	defer srv.Close()

	exec := NewExecutor(srv.Client(), stubCredentialResolver{})
	exec.catalogBase = srv.URL
	models, err := exec.ListModels(context.Background(), ports.NewRoutableTarget(
		"draft",
		"chatgpt",
		srv.URL+"/v1",
		"keychain:chatgpt/default",
		protocolkind.ChatCompletions,
		"credential_ref",
	))
	if err == nil {
		t.Fatalf("expected error, got models=%v", models)
	}
	if called {
		t.Fatal("catalog endpoint must not be called when tier is unknown")
	}
}

func TestListModels_TierResource404ReturnsError(t *testing.T) {
	t.Parallel()

	var seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	exec := NewExecutor(srv.Client(), stubCredentialResolver{})
	exec.catalogBase = srv.URL
	_, err := exec.ListModels(context.Background(), ports.NewRoutableTarget(
		"draft",
		"chatgpt",
		srv.URL+"/v1",
		"keychain:chatgpt/plus/sess_abc",
		protocolkind.ChatCompletions,
		"credential_ref",
	))
	if err == nil {
		t.Fatal("expected error")
	}
	if seenPath != "/api/v1/model-catalog/chatgpt/subscriptions/plus" {
		t.Fatalf("path=%q", seenPath)
	}
}

func TestExecute_UsesChatGPTCodexEndpointForOpenAIBaseURL(t *testing.T) {
	t.Parallel()

	rt := &captureRoundTripper{}
	exec := NewExecutor(&http.Client{Transport: rt}, stubCredentialResolver{})
	req := ports.NewProviderRequest(
		canonical.NewGenerationRequest(canonical.GenerationRequestParams{
			Model: "gpt-5.4-mini",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hello")},
		}),
		ports.NewExecutionContract(true),
		ports.NewRoutableTarget(
			"draft",
			string(providercatalog.ProviderSpecChatGPT),
			"https://api.openai.com/v1",
			"keychain:chatgpt/plus/sess_abc",
			protocolkind.Responses,
			"backend_chatgpt",
		),
	)
	resp, err := exec.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.EnvelopeStream() == nil {
		t.Fatal("expected envelope stream response")
	}
	if closeErr := resp.Close(); closeErr != nil {
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
		canonical.NewGenerationRequest(canonical.GenerationRequestParams{
			Model: "gpt-5.4-mini",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hello")},
		}),
		ports.NewExecutionContract(true),
		ports.NewRoutableTarget(
			"draft",
			string(providercatalog.ProviderSpecChatGPT),
			srv.URL+"/backend-api/codex",
			"keychain:chatgpt/plus/sess_abc",
			protocolkind.Responses,
			"backend_chatgpt",
		),
	)
	if _, err := exec.Execute(context.Background(), req); err != nil {
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
		canonical.NewGenerationRequest(canonical.GenerationRequestParams{
			Model: "gpt-5.4-mini",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hello")},
		}),
		ports.NewExecutionContract(true),
		ports.NewRoutableTarget(
			"draft",
			string(providercatalog.ProviderSpecChatGPT),
			srv.URL+"/backend-api/codex",
			"keychain:chatgpt/plus/sess_abc",
			protocolkind.Responses,
			"backend_chatgpt",
		),
	)
	_, err := exec.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "credential reference could not be resolved") {
		t.Fatalf("error=%v", err)
	}
}

func TestExecute_StreamingReturnsCanonicalStream(t *testing.T) {
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
		canonical.NewGenerationRequest(canonical.GenerationRequestParams{
			Model: "gpt-5.4-mini",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hello")},
		}),
		ports.NewExecutionContract(true),
		ports.NewRoutableTarget(
			"draft",
			string(providercatalog.ProviderSpecChatGPT),
			srv.URL+"/backend-api/codex",
			"keychain:chatgpt/plus/sess_abc",
			protocolkind.Responses,
			"backend_chatgpt",
		),
	)
	resp, err := exec.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.EnvelopeStream() == nil {
		t.Fatal("expected envelope stream response")
	}
	if closeErr := resp.Close(); closeErr != nil {
		t.Fatalf("close stream: %v", closeErr)
	}
}
