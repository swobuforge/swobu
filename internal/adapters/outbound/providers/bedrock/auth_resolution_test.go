package bedrock

import (
	"context"
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

type resolutionCredentialProvider struct {
	value string
	err   error
}

func TestDeadSupportedCredentialReferencesNeverResolveAsReady(t *testing.T) {
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "ambient-token")
	region, err := routing.ParseBedrockRegion("eu-west-2")
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"env:MISSING", "file:/missing/token", "secret:missing"} {
		t.Run(raw, func(t *testing.T) {
			connection, err := routing.NewBedrockConnection(region, "https://bedrock-mantle.eu-west-2.api.aws/v1", raw)
			if err != nil {
				t.Fatalf("real credential grammar rejected supported ref: %v", err)
			}
			_, err = resolveBedrockAuth(context.Background(), resolutionCredentialProvider{err: errors.New("not found")}, connection.Credential().String(), region.String())
			if err == nil {
				t.Fatal("dead credential reference became ready")
			}
		})
	}
}

func (p resolutionCredentialProvider) ResolveCredential(context.Context, string, string) (string, error) {
	return p.value, p.err
}

func TestResolveBedrockAuthRejectsDeadTargetReferenceBeforeAmbientFallback(t *testing.T) {
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "ambient-token")
	_, err := resolveBedrockAuth(context.Background(), resolutionCredentialProvider{err: errors.New("missing")}, "env:MISSING", "eu-west-2")
	if err == nil {
		t.Fatal("dead target credential became usable evidence")
	}
}

func TestResolveBedrockAuthCredentialReferenceSelectsBearer(t *testing.T) {
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "ambient-token")
	resolved, err := resolveBedrockAuth(context.Background(), resolutionCredentialProvider{value: "target-token"}, "secret:target", "eu-west-2")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.token != "target-token" {
		t.Fatalf("resolved token = %q", resolved.token)
	}
}

func TestResolveBedrockAuthReloadsAWSConfiguration(t *testing.T) {
	setStaticAWSCredentials(t, "first-access", "first-secret", "")
	first, err := resolveBedrockAuth(context.Background(), nil, "", "eu-west-2")
	if err != nil {
		t.Fatal(err)
	}
	firstCredentials, err := first.config.Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AWS_ACCESS_KEY_ID", "second-access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "second-secret")
	second, err := resolveBedrockAuth(context.Background(), nil, "", "eu-west-2")
	if err != nil {
		t.Fatal(err)
	}
	secondCredentials, err := second.config.Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if firstCredentials.AccessKeyID != "first-access" || secondCredentials.AccessKeyID != "second-access" {
		t.Fatalf("AWS config was not reloaded: first=%q second=%q", firstCredentials.AccessKeyID, secondCredentials.AccessKeyID)
	}
}

func setStaticAWSCredentials(t *testing.T, accessKey, secretKey, sessionToken string) {
	t.Helper()
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_ACCESS_KEY_ID", accessKey)
	t.Setenv("AWS_SECRET_ACCESS_KEY", secretKey)
	t.Setenv("AWS_SESSION_TOKEN", sessionToken)
}
