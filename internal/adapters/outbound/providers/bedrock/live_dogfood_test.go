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
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestLiveBedrockMantleCatalogAuthenticationPrecedence(t *testing.T) {
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

	if strings.TrimSpace(os.Getenv("AWS_BEARER_TOKEN_BEDROCK")) != "" {
		t.Run("explicit_api_key", func(t *testing.T) {
			assertLiveCatalog(t, ctx, exec, endpoint, "explicit_api_key", "env:AWS_BEARER_TOKEN_BEDROCK")
		})
	}
	t.Run("aws_identity", func(t *testing.T) {
		assertLiveCatalog(t, ctx, exec, endpoint, "aws_identity", "")
	})
}

func assertLiveCatalog(t *testing.T, ctx context.Context, exec BackendAdapter, endpoint string, authMode string, credentialRef string) {
	t.Helper()
	target := provider.NewTargetSnapshot(
		"live-bedrock-dogfood",
		string(profile.ProviderSpecBedrock),
		endpoint,
		credentialRef,
		protocolkind.Responses,

		profile.FrameHTTPJSONBody,
		"responses")

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
