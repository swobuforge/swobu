package bedrock

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func newBedrockTarget(baseURL, credentialRef string, kind protocolkind.ProtocolKind) provider.TargetSnapshot {
	return provider.NewTargetSnapshot(
		"backend-a",
		"bedrock",
		baseURL,
		credentialRef,
		kind,

		"",
		string(kind))

}

func TestBedrockMantleMessagesRejectsStructuredOutputBeforeTransport(t *testing.T) {
	format, err := canonical.NewOutputFormat(canonical.OutputFormatParams{
		Kind:   canonical.OutputFormatJSONSchema,
		Name:   "reply",
		Schema: canonical.NewRawJSONObject(`{"type":"object"}`),
		Strict: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:        canonical.Specify("model"),
		Items:        []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
		OutputFormat: canonical.Specify(format),
	})
	target := newBedrockTarget(
		"https://bedrock-mantle.us-east-1.api.aws/v1",
		"env:AWS_BEARER_TOKEN_BEDROCK",
		protocolkind.Messages,
	)
	target.Model = request.Model()
	backend, err := NewExecutor(nil).ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = backend.Codec.Encode(provider.Request{
		Canonical: request,
		Delivery:  delivery.BufferedDelivery(),
	})
	var incompatible provider.IncompatibleTargetError
	if !errors.As(err, &incompatible) {
		t.Fatalf("encode error = %T %v, want IncompatibleTargetError", err, err)
	}
}

func TestBedrockMantleNonMessagesProtocolsKeepTheirStructuredOutputSemantics(t *testing.T) {
	format, err := canonical.NewOutputFormat(canonical.OutputFormatParams{
		Kind:   canonical.OutputFormatJSONSchema,
		Name:   "reply",
		Schema: canonical.NewRawJSONObject(`{"type":"object"}`),
		Strict: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:        canonical.Specify("model"),
		Items:        []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")},
		OutputFormat: canonical.Specify(format),
	})
	for _, kind := range []protocolkind.ProtocolKind{
		protocolkind.ChatCompletions,
		protocolkind.Responses,
	} {
		t.Run(string(kind), func(t *testing.T) {
			target := newBedrockTarget(
				"https://bedrock-mantle.us-east-1.api.aws/v1",
				"env:AWS_BEARER_TOKEN_BEDROCK",
				kind,
			)
			target.Model = request.Model()
			backend, err := NewExecutor(nil).ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := backend.Codec.Encode(provider.Request{
				Canonical: request,
				Delivery:  delivery.BufferedDelivery(),
			}); err != nil {
				t.Fatalf("%s structured output rejected by Messages-only Mantle policy: %v", kind, err)
			}
		})
	}
}

func TestBedrockConverseEndpointIsNotMisrepresentedAsACompatibleMessagesTarget(t *testing.T) {
	called := false
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("transport must not be reached")
	})}
	target := newBedrockTarget(
		"https://bedrock-runtime.us-east-1.amazonaws.com",
		"env:AWS_BEARER_TOKEN_BEDROCK",
		protocolkind.Messages,
	)
	_, err := NewExecutor(client).Send(
		context.Background(),
		target,
		carrier.NewDocument(protocolkind.Messages, "application/json", nil, []byte(`{"output_config":{"format":{"type":"json_schema"}}}`), carrier.Meta{}),
	)
	var endpointErr canonical.Error
	if !errors.As(err, &endpointErr) || endpointErr.Code != canonical.ErrorCodeBadEndpoint {
		t.Fatalf("send error = %T %v, want unsupported endpoint error", err, err)
	}
	if called {
		t.Fatal("Bedrock Converse endpoint reached transport through Mantle adapter")
	}
}

func TestBedrockChatCompletionsUsesExactLegacyTokenFieldPolicy(t *testing.T) {
	maxTokens := 64
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{MaxOutputTokens: &maxTokens})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hi")}, Controls: controls})
	target := newBedrockTarget("https://bedrock-runtime.us-east-1.amazonaws.com", "env:AWS_BEARER_TOKEN_BEDROCK", protocolkind.ChatCompletions)
	target.Model = request.Model()
	backend, err := NewExecutor(nil).ResolveBackend(target)
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
	if payload["max_tokens"] != float64(maxTokens) {
		t.Fatalf("max_tokens = %#v, want %d", payload["max_tokens"], maxTokens)
	}
	if _, exists := payload["max_completion_tokens"]; exists {
		t.Fatalf("unexpected max_completion_tokens in %s", document.RawBytes())
	}
}

func TestBedrockMantleMessagesUsesInlineImagePolyfill(t *testing.T) {
	target := newBedrockTarget("https://bedrock-runtime.us-east-1.amazonaws.com", "env:AWS_BEARER_TOKEN_BEDROCK", protocolkind.Messages)
	target.Model = "model"
	backend, err := NewExecutor(nil).ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	requestForImage := func(image canonical.ImagePart) canonical.CanonicalRequest {
		message, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewImageMessagePart(image)})
		if err != nil {
			t.Fatal(err)
		}
		return canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("model"), Items: []canonical.CanonicalItem{message}})
	}
	inlineImage, _ := canonical.NewInlineImage(canonical.ImageMediaPNG, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, canonical.Unspecified[canonical.ImageDetail]())
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: requestForImage(inlineImage), Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatalf("Bedrock Mantle Messages rejected inline image: %v", err)
	}
	if !strings.Contains(string(document.RawBytes()), `"type":"base64"`) {
		t.Fatalf("inline image did not lower as base64: %s", document.RawBytes())
	}
	urlImage, err := canonical.NewURLImage("https://example.test/image.png", canonical.Unspecified[canonical.ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	resolveCalls := 0
	document, _, err = backend.Codec.Encode(provider.Request{
		Canonical: requestForImage(urlImage),
		Delivery:  delivery.BufferedDelivery(),
		EncodeContext: provider.EncodeContext{Context: context.Background(), ResolveImage: func(_ context.Context, source canonical.URLImage) (provider.InspectedImage, error) {
			resolveCalls++
			if source.String() != "https://example.test/image.png" {
				t.Fatalf("resolved URL = %q", source.String())
			}
			return provider.InspectedImage{MediaType: canonical.ImageMediaPNG, Bytes: []byte{1, 2, 3}}, nil
		}},
	})
	if err != nil {
		t.Fatalf("Bedrock Mantle Messages rejected resolved URL image: %v", err)
	}
	if resolveCalls != 1 || !strings.Contains(string(document.RawBytes()), `"type":"base64"`) || strings.Contains(string(document.RawBytes()), "https://example.test/image.png") {
		t.Fatalf("URL image lowering calls=%d document=%s", resolveCalls, document.RawBytes())
	}
}

func TestBedrockMessagesInheritsProtocolWebSearch(t *testing.T) {
	target := newBedrockTarget("https://bedrock-runtime.us-east-1.amazonaws.com", "env:AWS_BEARER_TOKEN_BEDROCK", protocolkind.Messages)
	target.Model = "model"
	backend, err := NewExecutor(nil).ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify(target.Model),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, canonical.NewWebSearchDeclaration()),
			canonicaltest.Message(t, canonical.MessageRoleUser, "search"),
		},
	})
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document.RawBytes()), `"type":"web_search_20260209"`) {
		t.Fatalf("Bedrock target did not inherit Messages web search: %s", document.RawBytes())
	}
}

func TestBedrockMessagesReplaysOpaqueThinking(t *testing.T) {
	opaque, err := canonical.NewMessagesOpaqueThinking([]byte(`{"type":"thinking","thinking":"brief","signature":"portable-claude-signature"}`))
	if err != nil {
		t.Fatal(err)
	}
	part, err := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "brief")
	if err != nil {
		t.Fatal(err)
	}
	reasoning, err := canonical.NewReasoningItem([]canonical.ReasoningPart{part}, opaque)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("anthropic.claude"),
		Items: []canonical.CanonicalItem{reasoning, canonicaltest.Message(t, canonical.MessageRoleUser, "again")},
	})
	target := newBedrockTarget("https://bedrock-runtime.us-east-1.amazonaws.com", "env:AWS_BEARER_TOKEN_BEDROCK", protocolkind.Messages)
	target.Model = request.Model()
	backend, err := NewExecutor(nil).ResolveBackend(target)
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := backend.Codec.Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document.RawBytes()), `"signature":"portable-claude-signature"`) {
		t.Fatalf("Bedrock Messages composition did not admit Claude thinking state: %s", document.RawBytes())
	}
}

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

func newBedrockProviderRequest(t *testing.T, baseURL, credentialRef string, kind protocolkind.ProtocolKind, providerDelivery delivery.Delivery) testProviderRequest {
	t.Helper()
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("openai.gpt-4.1-mini"),
		Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "ping")},
	})
	return newTestProviderRequest(
		"test-ex", protocolkind.Responses, request,
		carrier.Document{},
		exchange.NewExecutionContract(providerDelivery),
		newBedrockTarget(baseURL, credentialRef, kind),
	)
}

func executeBedrockProviderRequest(ctx context.Context, exec BackendAdapter, req testProviderRequest) (provider.Ingress, error) {
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

type recordingChanges = []compat.Change

type testCredentialProvider struct {
	token string
}

func (p testCredentialProvider) ResolveCredential(context.Context, string, string) (string, error) {
	if p.token != "" {
		return p.token, nil
	}
	return "test-token", nil
}

func mustJSONBodyMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return body
}

func TestProviderRequestPathForProtocol_MantleFamiliesOnly(t *testing.T) {
	t.Parallel()

	cases := map[protocolkind.ProtocolKind]string{
		protocolkind.Responses:       "/responses",
		protocolkind.ChatCompletions: "/chat/completions",
		protocolkind.Messages:        "/messages",
	}
	for kind, want := range cases {
		t.Run(kind.String(), func(t *testing.T) {
			got, err := profile.ProviderRequestPath(string(profile.ProviderSpecBedrock), kind)
			if err != nil {
				t.Fatalf("ProviderRequestPath(%s) error: %v", kind, err)
			}
			if got != want {
				t.Fatalf("ProviderRequestPath(%s) = %q want %q", kind, got, want)
			}
		})
	}
}

func TestValidateBedrockMantleEndpoint_AcceptsMantleAndLocalHosts(t *testing.T) {
	t.Parallel()

	if err := validateBedrockMantleEndpoint("https://bedrock-mantle.us-east-1.api.aws/v1"); err != nil {
		t.Fatalf("mantle endpoint rejected: %v", err)
	}
	if err := validateBedrockMantleEndpoint("http://127.0.0.1:1234/v1"); err != nil {
		t.Fatalf("local test endpoint rejected: %v", err)
	}
	if err := validateBedrockMantleEndpoint("https://bedrock.us-east-1.amazonaws.com/openai/v1"); err == nil {
		t.Fatal("expected non-Mantle host to be rejected")
	}
	if err := validateBedrockMantleEndpoint("https://bedrock.us-east-1.amazonaws.com"); err == nil {
		t.Fatal("expected control-plane host to be rejected")
	}
}

func TestListModels_AbsentCredentialReferenceIgnoresAmbientBearer(t *testing.T) {
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "ambient-token")
	t.Setenv("AWS_REGION", "eu-central-1")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")

	tmp := t.TempDir()
	awsDir := filepath.Join(tmp, ".aws")
	if err := os.MkdirAll(awsDir, 0o755); err != nil {
		t.Fatalf("mkdir ~/.aws: %v", err)
	}
	configPath := filepath.Join(awsDir, "config")
	if err := os.WriteFile(configPath, []byte("[default]\nregion = eu-central-1\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	credPath := filepath.Join(awsDir, "credentials")
	if err := os.WriteFile(credPath, []byte("[default]\naws_access_key_id = test\naws_secret_access_key = test\n"), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	t.Setenv("HOME", tmp)
	t.Setenv("AWS_CONFIG_FILE", configPath)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credPath)

	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/models" {
			t.Fatalf("path=%q want /models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "AWS4-HMAC-SHA256 ") {
			t.Fatalf("authorization=%q want SigV4", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"anthropic.claude-3-5-sonnet"},{"id":"amazon.nova-lite-v1"}]}`))
	}))
	defer upstream.Close()

	exec := NewExecutor(nil)
	models, err := exec.ListDeployments(context.Background(), newBedrockTarget(upstream.URL, "", protocolkind.Responses))
	if err != nil {
		t.Fatalf("ListDeployments error: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests=%d want=1", requests)
	}
	if len(models) != 2 || models[0].Name != "amazon.nova-lite-v1" || models[1].Name != "anthropic.claude-3-5-sonnet" {
		t.Fatalf("deployments=%v", models)
	}
}

func TestListModels_TargetCredential_PrecedesAmbientBearerToken(t *testing.T) {
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "ambient-token")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer target-token" {
			t.Fatalf("authorization=%q want target credential", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	}))
	defer upstream.Close()

	exec := NewExecutor(nil)
	exec.credentials = testCredentialProvider{token: "target-token"}
	if _, err := exec.ListDeployments(context.Background(), newBedrockTarget(upstream.URL, "secret:target", protocolkind.Responses)); err != nil {
		t.Fatalf("ListDeployments error: %v", err)
	}
}

func TestBedrockSigningRegion_DoesNotFallBackToEnvironment(t *testing.T) {
	t.Setenv("AWS_REGION", "eu-central-1")
	t.Setenv("AWS_DEFAULT_REGION", "eu-west-1")

	if _, err := bedrockSigningRegion(mustParseURL("http://127.0.0.1:1234/v1")); err == nil {
		t.Fatal("expected local endpoint without persisted region to be rejected")
	}
}

func TestListModels_AmbientAWS_DoesNotSetAcceptEncodingHeader(t *testing.T) {
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")

	tmp := t.TempDir()
	awsDir := filepath.Join(tmp, ".aws")
	if err := os.MkdirAll(awsDir, 0o755); err != nil {
		t.Fatalf("mkdir ~/.aws: %v", err)
	}
	configPath := filepath.Join(awsDir, "config")
	if err := os.WriteFile(configPath, []byte("[default]\nregion = eu-central-1\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	credPath := filepath.Join(awsDir, "credentials")
	if err := os.WriteFile(credPath, []byte("[default]\naws_access_key_id = test\naws_secret_access_key = test\n"), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	t.Setenv("HOME", tmp)
	t.Setenv("AWS_CONFIG_FILE", configPath)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credPath)

	capture := &captureRoundTripper{}
	exec := NewExecutor(&http.Client{Transport: capture})
	if _, err := exec.ListDeployments(context.Background(), newBedrockTarget("https://bedrock-mantle.eu-central-1.api.aws/v1", "", protocolkind.Responses)); err != nil {
		t.Fatalf("ListDeployments error: %v", err)
	}
	if capture.request == nil {
		t.Fatal("expected captured request")
	}
	if got := capture.request.Header.Get("Accept-Encoding"); got != "" {
		t.Fatalf("accept-encoding=%q want empty", got)
	}
}

func TestListModels_EnvMode_UsesMantleHTTP(t *testing.T) {
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "test-token")

	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/models" {
			t.Fatalf("path=%q want /models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization=%q want Bearer test-token", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	}))
	defer upstream.Close()

	exec := NewExecutor(nil)
	exec.credentials = testCredentialProvider{}
	models, err := exec.ListDeployments(context.Background(), newBedrockTarget(upstream.URL, "env:AWS_BEARER_TOKEN_BEDROCK", protocolkind.Responses))
	if err != nil {
		t.Fatalf("ListDeployments error: %v", err)
	}
	if len(models) != 1 || models[0].Name != "m1" {
		t.Fatalf("deployments=%v", models)
	}
	if requests != 1 {
		t.Fatalf("requests=%d want=1", requests)
	}
}

type captureRoundTripper struct {
	request       *http.Request
	statusCode    int
	responseBody  string
	responseError error
}

func (c *captureRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if c.responseError != nil {
		return nil, c.responseError
	}
	c.request = req.Clone(req.Context())
	body := c.responseBody
	if body == "" {
		body = `{"data":[{"id":"m1"}]}`
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

func TestSendProviderRequest_BufferedResponsesRoutesToMantlePath(t *testing.T) {
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "test-token")

	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method=%q want POST", r.Method)
		}
		if r.URL.Path != "/responses" {
			t.Fatalf("path=%q want /responses", r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("content-type=%q want application/json", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization=%q want Bearer test-token", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		_ = r.Body.Close()
		gotBody = append([]byte(nil), body...)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	exec := NewExecutor(upstream.Client())
	exec.credentials = testCredentialProvider{}
	ingress, err := executeBedrockProviderRequest(context.Background(), exec, newBedrockProviderRequest(
		t,
		upstream.URL,
		"env:AWS_BEARER_TOKEN_BEDROCK",
		protocolkind.Responses,
		delivery.BufferedDelivery(),
	))
	if err != nil {
		t.Fatalf("SendProviderRequest error: %v", err)
	}
	documentIngress, ok := ingress.(provider.DocumentIngress)
	if !ok {
		t.Fatalf("provider transport returned %T, want provider.DocumentIngress", ingress)
	}
	doc := documentIngress.Document
	if string(gotBody) == "" {
		t.Fatal("expected encoded request body")
	}
	if string(doc.RawBytes()) != `{"ok":true}` {
		t.Fatalf("response body=%q want %q", string(doc.RawBytes()), `{"ok":true}`)
	}
	if doc.Family != protocolkind.Responses {
		t.Fatalf("family=%q want responses", doc.Family)
	}
}

func TestSendProviderRequest_StreamingMessagesRoutesToMantlePath(t *testing.T) {
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "test-token")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method=%q want POST", r.Method)
		}
		if r.URL.Path != "/anthropic/v1/messages" {
			t.Fatalf("path=%q want /anthropic/v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Fatalf("anthropic-version=%q want 2023-06-01", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization=%q want Bearer test-token", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("stream-chunk-1"))
	}))
	defer upstream.Close()

	exec := NewExecutor(upstream.Client())
	exec.credentials = testCredentialProvider{}
	ingress, err := executeBedrockProviderRequest(context.Background(), exec, newBedrockProviderRequest(
		t,
		upstream.URL,
		"env:AWS_BEARER_TOKEN_BEDROCK",
		protocolkind.Messages,
		delivery.StreamingDelivery(delivery.FramingSSE),
	))
	if err != nil {
		t.Fatalf("SendProviderRequest error: %v", err)
	}
	streamIngress, ok := ingress.(provider.StreamIngress)
	if !ok {
		t.Fatalf("provider transport returned %T, want provider.StreamIngress", ingress)
	}
	stream := streamIngress.Stream
	if stream.MediaType != "text/event-stream" {
		t.Fatalf("media type=%q want text/event-stream", stream.MediaType)
	}
	raw, err := io.ReadAll(stream.Body)
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if string(raw) != "stream-chunk-1" {
		t.Fatalf("stream body=%q want stream-chunk-1", string(raw))
	}
	if err := stream.Body.Close(); err != nil {
		t.Fatalf("close stream: %v", err)
	}
}

func TestSendProviderRequest_BufferedMessagesDoesNotEmitCacheBreakpoints(t *testing.T) {
	sawBody := make(chan string, 1)
	sawErr := make(chan error, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method=%q want POST", r.Method)
		}
		if r.URL.Path != "/anthropic/v1/messages" {
			t.Fatalf("path=%q want /anthropic/v1/messages", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			sawErr <- err
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = r.Body.Close()
		sawBody <- string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","model":"openai.gpt-4.1-mini","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`))
	}))
	defer upstream.Close()

	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("openai.gpt-4.1-mini"),
		Items: []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleUser, "ping"),
		},
	})
	exec := NewExecutor(upstream.Client())
	exec.credentials = testCredentialProvider{token: "test-token"}
	sink := &recordingChanges{}
	req := newTestProviderRequest(
		"test-ex", protocolkind.Responses, request,
		carrier.Document{},
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		newBedrockTarget(upstream.URL, "secret:test", protocolkind.Messages),
		sink,
	)
	req.ExchangeID = "ex-bedrock-cache-breakpoint"

	ingress, err := executeBedrockProviderRequest(context.Background(), exec, req)
	if err != nil {
		t.Fatalf("SendProviderRequest error: %v", err)
	}
	documentIngress, ok := ingress.(provider.DocumentIngress)
	if !ok {
		t.Fatalf("provider transport returned %T, want provider.DocumentIngress", ingress)
	}
	doc := documentIngress.Document
	if string(doc.RawBytes()) == "" {
		t.Fatal("expected upstream response body")
	}

	var body string
	select {
	case err := <-sawErr:
		t.Fatalf("read request body: %v", err)
	case body = <-sawBody:
	}
	payload := mustJSONBodyMap(t, []byte(body))
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages=%T len=%d want one message", payload["messages"], len(messages))
	}
	firstMsg, ok := messages[0].(map[string]any)
	if !ok {
		t.Fatalf("messages[0]=%T want map[string]any", messages[0])
	}
	content, ok := firstMsg["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("content=%T len=%d want one content block", firstMsg["content"], len(content))
	}
	if _, ok := content[0].(map[string]any); !ok {
		t.Fatalf("content[0]=%T want map[string]any", content[0])
	}

	if len(*sink) != 0 {
		t.Fatalf("captured effects len=%d want 0", len(*sink))
	}
}

func TestBedrockEndpointClassAndRegion_MantleOnly(t *testing.T) {
	t.Parallel()

	class, region := bedrockEndpointClassAndRegion("https://bedrock-mantle.us-east-1.api.aws/v1")
	if class != "bedrock_mantle_openai_compat" || region != "us-east-1" {
		t.Fatalf("class=%q region=%q", class, region)
	}
	class, region = bedrockEndpointClassAndRegion("https://bedrock.us-west-2.amazonaws.com/openai/v1")
	if class != "unknown" || region != "" {
		t.Fatalf("class=%q region=%q want unknown/empty for non-Mantle host", class, region)
	}
}
