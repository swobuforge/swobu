//go:build integration_live

package bedrock

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/profile"
)

func TestLiveBedrockMantleCatalogAuthModes(t *testing.T) {
	if strings.TrimSpace(os.Getenv("SWOBU_LIVE_BEDROCK_DOGFOOD")) != "1" {
		t.Skip("set SWOBU_LIVE_BEDROCK_DOGFOOD=1 to probe live Bedrock Mantle auth modes")
	}
	region := firstNonEmpty(os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION"), "us-east-1")
	endpoint := firstNonEmpty(os.Getenv("SWOBU_BEDROCK_MANTLE_ENDPOINT"), profile.BedrockMantleEndpointForRegion(region))
	if endpoint == "" {
		t.Fatal("Bedrock Mantle endpoint is empty")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	exec := NewExecutor(http.DefaultClient)

	if strings.TrimSpace(os.Getenv("AWS_BEARER_TOKEN_BEDROCK")) == "" {
		t.Run("api_key_env", func(t *testing.T) {
			t.Skip("AWS_BEARER_TOKEN_BEDROCK is not set")
		})
	} else {
		t.Run("api_key_env", func(t *testing.T) {
			assertLiveCatalog(t, ctx, exec, endpoint, string(profile.AuthModeEnv), "env:AWS_BEARER_TOKEN_BEDROCK")
		})
	}
	if strings.TrimSpace(os.Getenv("AWS_ACCESS_KEY_ID")) == "" || strings.TrimSpace(os.Getenv("AWS_SECRET_ACCESS_KEY")) == "" {
		t.Run("aws_env_session", func(t *testing.T) {
			t.Skip("AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY are not set")
		})
	} else {
		t.Run("aws_env_session", func(t *testing.T) {
			assertLiveCatalog(t, ctx, exec, endpoint, string(profile.AuthModeAWSEnvSession), "")
		})
	}
	profileName := firstNonEmpty(os.Getenv("AWS_PROFILE"), "default")
	t.Run("aws_profile", func(t *testing.T) {
		assertLiveCatalog(t, ctx, exec, endpoint, string(profile.AuthModeAWSProfile), profile.BedrockProfileCredentialRef(profileName))
	})
}

func assertLiveCatalog(t *testing.T, ctx context.Context, exec ProviderIngressResolverAdapter, endpoint string, authMode string, credentialRef string) {
	t.Helper()
	target := exchange.NewRoutableTarget(
		"live-bedrock-dogfood",
		string(profile.ProviderSpecBedrock),
		endpoint,
		credentialRef,
		protocolkind.Responses,
		string(profile.AuthCredentialRef),
		profile.FrameHTTPJSONBody,
		"responses",
	)
	target.AuthMode = authMode
	models, err := exec.ListDeployments(ctx, target)
	if err != nil {
		t.Fatalf("%s catalog probe failed: %v", authMode, err)
	}
	if len(models) == 0 {
		t.Fatalf("%s catalog probe returned no models", authMode)
	}
	t.Logf("%s catalog probe returned %d models", authMode, len(models))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
