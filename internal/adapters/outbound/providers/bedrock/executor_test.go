package bedrock

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/ports"
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

func TestListModels_AWSProfileMode_UsesSDKCatalogPath(t *testing.T) {
	original := bedrockListFoundationModelIDs
	t.Cleanup(func() { bedrockListFoundationModelIDs = original })
	bedrockListFoundationModelIDs = func(ctx context.Context, cfg aws.Config) ([]string, error) {
		return []string{"amazon.nova-lite-v1", "anthropic.claude-3-5-sonnet"}, nil
	}
	t.Setenv("AWS_REGION", "eu-central-1")
	t.Setenv("AWS_ACCESS_KEY_ID", "ASIAFAKEACCESSKEY000")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "fake-secret-key")
	t.Setenv("AWS_SESSION_TOKEN", "fake-session-token")

	exec := NewExecutor(nil)
	models, err := exec.ListModels(context.Background(), ports.NewRoutableTarget(
		"backend-a", "bedrock", "https://bedrock-runtime.eu-central-1.amazonaws.com/openai/v1", "", protocolkind.ChatCompletions, "",
	))
	if err != nil {
		t.Fatalf("ListModels error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models len=%d want=2", len(models))
	}
}

func TestListModels_EnvMode_UsesHTTPModelsPath(t *testing.T) {
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "test-token")
	getCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/openai/v1/models" {
			t.Fatalf("method/path=%s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization=%q", r.Header.Get("Authorization"))
		}
		getCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	}))
	defer upstream.Close()

	exec := NewExecutor(upstream.Client())
	models, err := exec.ListModels(context.Background(), ports.NewRoutableTarget(
		"backend-a", "bedrock", upstream.URL+"/openai/v1", "env:AWS_BEARER_TOKEN_BEDROCK", protocolkind.ChatCompletions, "",
	))
	if err != nil {
		t.Fatalf("ListModels error: %v", err)
	}
	if getCalls != 1 || len(models) != 1 || models[0] != "m1" {
		t.Fatalf("calls=%d models=%v", getCalls, models)
	}
}
