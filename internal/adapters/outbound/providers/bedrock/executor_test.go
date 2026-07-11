package bedrock

import (
	"context"
	"github.com/swobuforge/swobu/internal/delivery"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
)

func TestBedrockSigningRegion_FromEnv(t *testing.T) {
	t.Setenv("AWS_REGION", "us-west-2")
	t.Setenv("AWS_DEFAULT_REGION", "")

	u, _ := url.Parse("https://bedrock-runtime.eu-central-1.amazonaws.com/openai/v1")
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

	u, _ := url.Parse("https://bedrock-runtime.eu-central-1.amazonaws.com/openai/v1")
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

func TestValidateBedrockOpenAIEndpoint_RejectsControlPlaneHost(t *testing.T) {
	err := validateBedrockRuntimeEndpoint("https://bedrock.us-east-1.amazonaws.com")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateBedrockOpenAIEndpoint_AcceptsRuntimeAndMantle(t *testing.T) {
	if err := validateBedrockRuntimeEndpoint("https://bedrock-runtime.us-east-1.amazonaws.com"); err != nil {
		t.Fatalf("runtime endpoint rejected: %v", err)
	}
	if err := validateBedrockRuntimeEndpoint("https://bedrock-mantle.us-east-1.api.aws"); err != nil {
		t.Fatalf("mantle endpoint rejected: %v", err)
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

func TestParseBedrockAuthMode_DefaultsToAWSProfile(t *testing.T) {
	mode, value := parseBedrockAuthMode("")
	if mode != "aws_profile" || value != "" {
		t.Fatalf("mode=%q value=%q", mode, value)
	}
}

func TestParseBedrockAuthMode_ProfileRef(t *testing.T) {
	mode, value := parseBedrockAuthMode("profile:work-prod")
	if mode != "aws_profile" || value != "work-prod" {
		t.Fatalf("mode=%q value=%q", mode, value)
	}
}

func TestParseBedrockAuthMode_APIKeyEnvRef(t *testing.T) {
	mode, value := parseBedrockAuthMode("env:AWS_BEARER_TOKEN_BEDROCK")
	if mode != "api_key_env" || value != "AWS_BEARER_TOKEN_BEDROCK" {
		t.Fatalf("mode=%q value=%q", mode, value)
	}
}

func TestParseBedrockAuthMode_AWSEnvSessionRef(t *testing.T) {
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

func TestListModels_AWSProfileMode_UsesControlPlaneHTTP(t *testing.T) {
	t.Setenv("AWS_REGION", "eu-central-1")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_ACCESS_KEY_ID", "ASIAFAKEACCESSKEY000")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fake-secret-key")
	t.Setenv("AWS_SESSION_TOKEN", "fake-session-token")
	originalControlURL := bedrockControlPlaneBaseURL
	t.Cleanup(func() { bedrockControlPlaneBaseURL = originalControlURL })

	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/foundation-models" {
			t.Fatalf("path=%q want /foundation-models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "AWS4-HMAC-SHA256 ") {
			t.Fatalf("authorization=%q want SigV4 header", got)
		}
		if got := r.Header.Get("X-Amz-Security-Token"); got == "" {
			t.Fatal("expected X-Amz-Security-Token")
		}
		_, _ = w.Write([]byte(`{"modelSummaries":[{"modelId":"amazon.nova-lite-v1"},{"modelId":"anthropic.claude-3-5-sonnet"}]}`))
	}))
	defer upstream.Close()
	bedrockControlPlaneBaseURL = func(region string) string {
		if region != "eu-central-1" {
			t.Fatalf("region=%q want eu-central-1", region)
		}
		return upstream.URL
	}

	exec := NewExecutor(nil)
	models, err := exec.ListModels(context.Background(), exchange.NewRoutableTarget(
		"backend-a", "bedrock", "https://bedrock-runtime.eu-central-1.amazonaws.com/openai/v1", "", protocolkind.ChatCompletions, "", "", "",
	))
	if err != nil {
		t.Fatalf("ListModels error: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests=%d want=1", requests)
	}
	if len(models) != 2 || models[0] != "amazon.nova-lite-v1" {
		t.Fatalf("models=%v", models)
	}
}

func TestListModels_EnvMode_UsesControlPlaneHTTP(t *testing.T) {
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "test-token")
	t.Setenv("AWS_REGION", "us-east-1")
	originalControlURL := bedrockControlPlaneBaseURL
	t.Cleanup(func() { bedrockControlPlaneBaseURL = originalControlURL })

	requests := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/foundation-models" {
			t.Fatalf("path=%q want /foundation-models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization=%q want Bearer test-token", got)
		}
		_, _ = w.Write([]byte(`{"modelSummaries":[{"modelId":"m1"}]}`))
	}))
	defer upstream.Close()
	bedrockControlPlaneBaseURL = func(region string) string {
		if region != "us-east-1" {
			t.Fatalf("region=%q want us-east-1", region)
		}
		return upstream.URL
	}

	exec := NewExecutor(nil)
	models, err := exec.ListModels(context.Background(), exchange.NewRoutableTarget(
		"backend-a", "bedrock", "https://bedrock-runtime.us-east-1.amazonaws.com/openai/v1", "env:AWS_BEARER_TOKEN_BEDROCK", protocolkind.ChatCompletions, "", "", "",
	))
	if err != nil {
		t.Fatalf("ListModels error: %v", err)
	}
	if len(models) != 1 || models[0] != "m1" {
		t.Fatalf("models=%v", models)
	}
	if requests != 1 {
		t.Fatalf("requests=%d want=1", requests)
	}
}

func TestListModels_EnvMode_MissingTokenFails(t *testing.T) {
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	t.Setenv("AWS_REGION", "us-east-1")

	exec := NewExecutor(nil)
	_, err := exec.ListModels(context.Background(), exchange.NewRoutableTarget(
		"backend-a", "bedrock", "https://bedrock-runtime.us-east-1.amazonaws.com/openai/v1", "env:AWS_BEARER_TOKEN_BEDROCK", protocolkind.ChatCompletions, "", "", "",
	))
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got != "BAD_ENDPOINT: bedrock API key env var is missing: AWS_BEARER_TOKEN_BEDROCK" {
		t.Fatalf("error=%q", got)
	}
}

func TestBedrockEndpointClassAndRegion(t *testing.T) {
	class, region := bedrockEndpointClassAndRegion("https://bedrock-mantle.us-east-1.api.aws/v1")
	if class != "bedrock_mantle_openai_compat" || region != "us-east-1" {
		t.Fatalf("class=%q region=%q", class, region)
	}
	class, region = bedrockEndpointClassAndRegion("https://bedrock-runtime.eu-west-2.amazonaws.com/openai/v1")
	if class != "bedrock_runtime_openai_compat" || region != "eu-west-2" {
		t.Fatalf("class=%q region=%q", class, region)
	}
}

func TestBedrockModelIDFromPayload(t *testing.T) {
	raw := []byte(`{"model":"openai.gpt-oss-20b","messages":[{"role":"user","content":"ping"}]}`)
	if got := bedrockModelIDFromPayload(raw); got != "openai.gpt-oss-20b" {
		t.Fatalf("model id=%q", got)
	}
	if got := bedrockModelIDFromPayload([]byte(`{"messages":[]}`)); got != "" {
		t.Fatalf("model id=%q want empty", got)
	}
}

func TestBedrockModelARNCandidates(t *testing.T) {
	foundation, inference := bedrockModelARNCandidates("us-east-1", "amazon.nova-lite-v1:0")
	if foundation != "arn:aws:bedrock:us-east-1::foundation-model/amazon.nova-lite-v1:0" {
		t.Fatalf("foundation=%q", foundation)
	}
	if inference != "arn:aws:bedrock:us-east-1::inference-profile/amazon.nova-lite-v1:0" {
		t.Fatalf("inference=%q", inference)
	}
}

func TestResolveBedrockOperation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		variant          string
		wantInvoke       bool
		wantDeliveryMode delivery.Mode
		wantErr          bool
	}{
		{name: "converse", variant: "converse"},
		{name: "converse stream", variant: "converse_stream", wantDeliveryMode: delivery.Streaming},
		{name: "invoke model", variant: "invoke_model", wantInvoke: true},
		{name: "invoke model stream", variant: "invoke_model_stream", wantInvoke: true, wantDeliveryMode: delivery.Streaming},
		{name: "empty rejected", variant: "", wantErr: true},
		{name: "protocol auto rejected", variant: "auto", wantErr: true},
		{name: "unsupported rejected", variant: "responses", wantErr: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveBedrockOperation(tc.variant)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveBedrockOperation(%q) expected error", tc.variant)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveBedrockOperation(%q) error: %v", tc.variant, err)
			}
			if got.invokeModel != tc.wantInvoke || got.deliveryMode != tc.wantDeliveryMode {
				t.Fatalf("resolveBedrockOperation(%q)=%+v want invoke=%v deliveryMode=%v", tc.variant, got, tc.wantInvoke, tc.wantDeliveryMode)
			}
		})
	}
}
