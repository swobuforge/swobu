package bedrock

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	exchangeruntime "github.com/swobuforge/swobu/internal/adapters/wire/exchangeruntime"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/ports"
	"github.com/swobuforge/swobu/internal/profile"
)

func newBedrockTarget(baseURL, credentialRef string, kind protocolkind.ProtocolKind) exchange.RoutableTarget {
	return exchange.NewRoutableTarget(
		"backend-a",
		"bedrock",
		baseURL,
		credentialRef,
		kind,
		"",
		"",
		string(kind),
	)
}

func newBedrockProviderRequest(t *testing.T, baseURL, credentialRef string, kind protocolkind.ProtocolKind, providerDelivery delivery.Delivery) ports.ProviderRequest {
	t.Helper()
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:     "openai.gpt-4.1-mini",
		InputText: "ping",
	})
	codec := exchangeruntime.NewResolver().ProviderRequestDocumentEncoder(kind)
	if codec == nil {
		t.Fatalf("provider request encoder missing for protocol %s", kind)
	}
	wireRequestResult, err := codec.EncodeProviderRequestDocument(request, providerDelivery, "")
	if err != nil {
		t.Fatalf("encode provider request document: %v", err)
	}
	return ports.NewProviderRequest(
		request,
		wireRequestResult.Value,
		exchange.NewExecutionContract(providerDelivery),
		newBedrockTarget(baseURL, credentialRef, kind),
	)
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

func TestProviderRequestPathForProtocol_MantleFamiliesOnly(t *testing.T) {
	t.Parallel()

	cases := map[protocolkind.ProtocolKind]string{
		protocolkind.Responses:       "/responses",
		protocolkind.ChatCompletions: "/chat/completions",
		protocolkind.Messages:        "/messages",
	}
	for kind, want := range cases {
		t.Run(kind.String(), func(t *testing.T) {
			got, err := exchangeruntime.ProviderRequestPath(string(profile.ProviderSpecBedrock), kind)
			if err != nil {
				t.Fatalf("ProviderRequestPath(%s) error: %v", kind, err)
			}
			if got != want {
				t.Fatalf("ProviderRequestPath(%s) = %q want %q", kind, got, want)
			}
		})
	}
	if _, err := exchangeruntime.ProviderRequestPath(string(profile.ProviderSpecBedrock), protocolkind.Completions); err == nil {
		t.Fatal("expected unsupported protocol to fail")
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

func TestBedrockSigningRegion_FromEnv(t *testing.T) {
	t.Setenv("AWS_REGION", "us-west-2")
	t.Setenv("AWS_DEFAULT_REGION", "")

	u, _ := url.Parse("https://bedrock-mantle.eu-central-1.api.aws/openai/v1")
	got, err := bedrockSigningRegion(context.Background(), u, "")
	if err != nil {
		t.Fatalf("bedrockSigningRegion error: %v", err)
	}
	if got != "us-west-2" {
		t.Fatalf("bedrockSigningRegion=%q want us-west-2", got)
	}
}

func TestBedrockSigningRegion_FromHost(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")

	u, _ := url.Parse("https://bedrock-mantle.eu-central-1.api.aws/openai/v1")
	got, err := bedrockSigningRegion(context.Background(), u, "")
	if err != nil {
		t.Fatalf("bedrockSigningRegion error: %v", err)
	}
	if got != "eu-central-1" {
		t.Fatalf("bedrockSigningRegion=%q want eu-central-1", got)
	}
}

func TestBedrockSigningRegion_RejectsUnknownHostWithoutEnv(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "missing-config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "missing-credentials"))
	t.Setenv("AWS_PROFILE", "swobu-bedrock-missing")

	u, _ := url.Parse("https://example.test/openai/v1")
	_, err := bedrockSigningRegion(context.Background(), u, "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBedrockSigningRegion_FromSDKProfileConfig(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config")
	contents := "[profile swobu-bedrock-test]\nregion = eu-west-2\n"
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("AWS_CONFIG_FILE", configPath)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(tmp, "credentials"))

	u, _ := url.Parse("https://example.test/openai/v1")
	got, err := bedrockSigningRegion(context.Background(), u, "swobu-bedrock-test")
	if err != nil {
		t.Fatalf("bedrockSigningRegion error: %v", err)
	}
	if got != "eu-west-2" {
		t.Fatalf("bedrockSigningRegion=%q want eu-west-2", got)
	}
}

func TestApplyBedrockAuth_ProfileRefWithExplicitRegionUsesSigV4(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")

	tmp := t.TempDir()
	awsDir := filepath.Join(tmp, ".aws")
	if err := os.MkdirAll(awsDir, 0o755); err != nil {
		t.Fatalf("mkdir ~/.aws: %v", err)
	}
	configPath := filepath.Join(awsDir, "config")
	if err := os.WriteFile(configPath, []byte("[profile swobu-bedrock]\nregion = us-east-1\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	credPath := filepath.Join(awsDir, "credentials")
	if err := os.WriteFile(credPath, []byte("[swobu-bedrock]\naws_access_key_id = test\naws_secret_access_key = test\n"), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	t.Setenv("HOME", tmp)
	t.Setenv("AWS_CONFIG_FILE", configPath)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credPath)

	req, err := http.NewRequest(http.MethodPost, "https://example.test/v1/responses", strings.NewReader(`{"inputText":"ping"}`))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if err := applyBedrockAuth(context.Background(), "profile:swobu-bedrock@eu-west-2", req, []byte(`{"inputText":"ping"}`)); err != nil {
		t.Fatalf("applyBedrockAuth error: %v", err)
	}
	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 ") {
		t.Fatalf("authorization=%q want SigV4 header", auth)
	}
	if !strings.Contains(auth, "/eu-west-2/bedrock/aws4_request") {
		t.Fatalf("authorization=%q want eu-west-2 signing scope", auth)
	}
}

func TestParseBedrockAuthMode_DefaultsToAWSProfile(t *testing.T) {
	t.Parallel()

	mode, value := parseBedrockAuthMode("")
	if mode != "aws_profile" || value != "" {
		t.Fatalf("mode=%q value=%q", mode, value)
	}
}

func TestParseBedrockAuthMode_ProfileRef(t *testing.T) {
	t.Parallel()

	mode, value := parseBedrockAuthMode("profile:work-prod")
	if mode != "aws_profile" || value != "work-prod" {
		t.Fatalf("mode=%q value=%q", mode, value)
	}
}

func TestParseBedrockAuthMode_APIKeyEnvRef(t *testing.T) {
	t.Parallel()

	mode, value := parseBedrockAuthMode("env:AWS_BEARER_TOKEN_BEDROCK")
	if mode != "api_key_env" || value != "AWS_BEARER_TOKEN_BEDROCK" {
		t.Fatalf("mode=%q value=%q", mode, value)
	}
}

func TestParseBedrockAuthMode_AWSEnvSessionRef(t *testing.T) {
	t.Parallel()

	mode, value := parseBedrockAuthMode("aws_env_session")
	if mode != "aws_profile" || value != "" {
		t.Fatalf("mode=%q value=%q", mode, value)
	}
}

func TestLoadBedrockAWSConfig_ProfileFallbackToDefaultSharedFiles(t *testing.T) {
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("HOME", t.TempDir())
	home := os.Getenv("HOME")
	awsDir := filepath.Join(home, ".aws")
	if err := os.MkdirAll(awsDir, 0o755); err != nil {
		t.Fatalf("mkdir ~/.aws: %v", err)
	}
	configPath := filepath.Join(awsDir, "config")
	if err := os.WriteFile(configPath, []byte("[profile swobu-bedrock]\nregion = us-east-1\n"), 0o600); err != nil {
		t.Fatalf("write ~/.aws/config: %v", err)
	}
	credPath := filepath.Join(awsDir, "credentials")
	if err := os.WriteFile(credPath, []byte("[swobu-bedrock]\naws_access_key_id = test\naws_secret_access_key = test\n"), 0o600); err != nil {
		t.Fatalf("write ~/.aws/credentials: %v", err)
	}

	injectedDir := t.TempDir()
	injectedConfigPath := filepath.Join(injectedDir, "config")
	injectedCredsPath := filepath.Join(injectedDir, "credentials")
	if err := os.WriteFile(injectedConfigPath, []byte("[default]\nregion = us-west-2\n"), 0o600); err != nil {
		t.Fatalf("write injected config: %v", err)
	}
	if err := os.WriteFile(injectedCredsPath, []byte("[default]\naws_access_key_id = injected\naws_secret_access_key = injected\n"), 0o600); err != nil {
		t.Fatalf("write injected credentials: %v", err)
	}
	t.Setenv("AWS_CONFIG_FILE", injectedConfigPath)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", injectedCredsPath)

	cfg, err := loadBedrockAWSConfig(context.Background(), "us-east-1", bedrockAuthModeAWSProfile, "swobu-bedrock")
	if err != nil {
		t.Fatalf("loadBedrockAWSConfig error: %v", err)
	}
	creds, credErr := cfg.Credentials.Retrieve(context.Background())
	if credErr != nil {
		t.Fatalf("retrieve creds error: %v", credErr)
	}
	if creds.AccessKeyID != "test" {
		t.Fatalf("access key id=%q want test", creds.AccessKeyID)
	}
}

func TestListModels_AWSProfileMode_UsesMantleHTTP(t *testing.T) {
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
	if err := os.WriteFile(configPath, []byte("[profile swobu-bedrock]\nregion = eu-central-1\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	credPath := filepath.Join(awsDir, "credentials")
	if err := os.WriteFile(credPath, []byte("[swobu-bedrock]\naws_access_key_id = test\naws_secret_access_key = test\n"), 0o600); err != nil {
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
			t.Fatalf("authorization=%q want SigV4 header", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"anthropic.claude-3-5-sonnet"},{"id":"amazon.nova-lite-v1"}]}`))
	}))
	defer upstream.Close()

	exec := NewExecutor(nil)
	models, err := exec.ListDeployments(context.Background(), newBedrockTarget(upstream.URL, "profile:swobu-bedrock@eu-central-1", protocolkind.Responses))
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

func TestListModels_AWSProfileMode_DoesNotSetAcceptEncodingHeader(t *testing.T) {
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
	if err := os.WriteFile(configPath, []byte("[profile swobu-bedrock]\nregion = eu-central-1\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	credPath := filepath.Join(awsDir, "credentials")
	if err := os.WriteFile(credPath, []byte("[swobu-bedrock]\naws_access_key_id = test\naws_secret_access_key = test\n"), 0o600); err != nil {
		t.Fatalf("write credentials: %v", err)
	}
	t.Setenv("HOME", tmp)
	t.Setenv("AWS_CONFIG_FILE", configPath)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credPath)

	capture := &captureRoundTripper{}
	exec := NewExecutor(&http.Client{Transport: capture})
	if _, err := exec.ListDeployments(context.Background(), newBedrockTarget("https://bedrock-mantle.eu-central-1.api.aws/v1", "profile:swobu-bedrock@eu-central-1", protocolkind.Responses)); err != nil {
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

func TestResolveProviderIngress_BufferedResponsesRoutesToMantlePath(t *testing.T) {
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
	ingress, err := exec.ResolveProviderIngress(context.Background(), newBedrockProviderRequest(
		t,
		upstream.URL,
		"env:AWS_BEARER_TOKEN_BEDROCK",
		protocolkind.Responses,
		delivery.BufferedDelivery(),
	))
	if err != nil {
		t.Fatalf("ResolveProviderIngress error: %v", err)
	}
	doc, ok := ingress.(carrier.WireDocument)
	if !ok {
		t.Fatalf("ResolveProviderIngress returned %T, want carrier.WireDocument", ingress)
	}
	if string(gotBody) == "" {
		t.Fatal("expected encoded request body")
	}
	if string(doc.RawBytes()) != `{"ok":true}` {
		t.Fatalf("response body=%q want %q", string(doc.RawBytes()), `{"ok":true}`)
	}
	if doc.Family != protocolkind.Responses {
		t.Fatalf("family=%q want responses", doc.Family)
	}
	if doc.Stage != carrier.StageProviderIngressIn {
		t.Fatalf("stage=%q want provider ingress in", doc.Stage)
	}
}

func TestResolveProviderIngress_StreamingMessagesRoutesToMantlePath(t *testing.T) {
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "test-token")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method=%q want POST", r.Method)
		}
		if r.URL.Path != "/messages" {
			t.Fatalf("path=%q want /messages", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization=%q want Bearer test-token", got)
		}
		_, _ = w.Write([]byte("stream-chunk-1"))
	}))
	defer upstream.Close()

	exec := NewExecutor(upstream.Client())
	ingress, err := exec.ResolveProviderIngress(context.Background(), newBedrockProviderRequest(
		t,
		upstream.URL,
		"env:AWS_BEARER_TOKEN_BEDROCK",
		protocolkind.Messages,
		delivery.StreamingDelivery(delivery.FramingSSE),
	))
	if err != nil {
		t.Fatalf("ResolveProviderIngress error: %v", err)
	}
	stream, ok := ingress.(carrier.WireStream)
	if !ok {
		t.Fatalf("ResolveProviderIngress returned %T, want carrier.WireStream", ingress)
	}
	if stream.Family != protocolkind.Messages {
		t.Fatalf("family=%q want messages", stream.Family)
	}
	if stream.Framing != carrier.FramingSSE {
		t.Fatalf("framing=%q want sse", stream.Framing)
	}
	frame, err := stream.Frames.Next(context.Background())
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}
	if string(frame.Data) != "stream-chunk-1" {
		t.Fatalf("frame data=%q want stream-chunk-1", string(frame.Data))
	}
	if err := stream.Frames.Close(); err != nil {
		t.Fatalf("close stream: %v", err)
	}
}

func TestResolveProviderIngress_BufferedMessagesDoesNotEmitCacheBreakpoints(t *testing.T) {
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "test-token")

	sawBody := make(chan string, 1)
	sawErr := make(chan error, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method=%q want POST", r.Method)
		}
		if r.URL.Path != "/messages" {
			t.Fatalf("path=%q want /messages", r.URL.Path)
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
		Model: "openai.gpt-4.1-mini",
		Items: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorUser, "ping"),
		},
	})
	codec := exchangeruntime.NewResolver().ProviderRequestDocumentEncoder(protocolkind.Messages)
	if codec == nil {
		t.Fatal("provider request encoder missing for messages")
	}
	wireRequestResult, err := codec.EncodeProviderRequestDocument(request, delivery.BufferedDelivery(), "")
	if err != nil {
		t.Fatalf("encode provider request document: %v", err)
	}
	exec := NewExecutor(upstream.Client())
	sink := &recordingEffectSink{}
	req := ports.NewProviderRequest(
		request,
		wireRequestResult.Value,
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		newBedrockTarget(upstream.URL, "env:AWS_BEARER_TOKEN_BEDROCK", protocolkind.Messages),
		sink,
	)
	req.ExchangeID = "ex-bedrock-cache-breakpoint"

	ingress, err := exec.ResolveProviderIngress(context.Background(), req)
	if err != nil {
		t.Fatalf("ResolveProviderIngress error: %v", err)
	}
	doc, ok := ingress.(carrier.WireDocument)
	if !ok {
		t.Fatalf("ResolveProviderIngress returned %T, want carrier.WireDocument", ingress)
	}
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

	if len(sink.effects) != 0 {
		t.Fatalf("captured effects len=%d want 0", len(sink.effects))
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

func TestBedrockModelIDFromPayload(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"model":"openai.gpt-oss-20b","messages":[{"role":"user","content":"ping"}]}`)
	if got := bedrockModelIDFromPayload(raw); got != "openai.gpt-oss-20b" {
		t.Fatalf("model id=%q", got)
	}
	if got := bedrockModelIDFromPayload([]byte(`{"messages":[]}`)); got != "" {
		t.Fatalf("model id=%q want empty", got)
	}
}

func TestBedrockModelARNCandidates(t *testing.T) {
	t.Parallel()

	foundation, inference := bedrockModelARNCandidates("us-east-1", "amazon.nova-lite-v1:0")
	if foundation != "arn:aws:bedrock:us-east-1::foundation-model/amazon.nova-lite-v1:0" {
		t.Fatalf("foundation=%q", foundation)
	}
	if inference != "arn:aws:bedrock:us-east-1::inference-profile/amazon.nova-lite-v1:0" {
		t.Fatalf("inference=%q", inference)
	}
}
