package bedrock

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/ports"
)

type bedrockAuthMode string

const (
	bedrockAuthModeAWSProfile bedrockAuthMode = "aws_profile"
	bedrockAuthModeAPIKeyEnv  bedrockAuthMode = "api_key_env"
)

func listBedrockModelsViaSDK(ctx context.Context, target ports.RoutableTarget, profile string) ([]string, error) {
	if trimBedrockInput(target.BaseURL) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("bedrock provider base URL is required")
	}
	region, err := bedrockSigningRegion(ctx, mustParseURL(target.BaseURL), profile)
	if err != nil {
		return nil, err
	}
	cfg, err := loadBedrockAWSConfig(ctx, region, profile)
	if err != nil {
		return nil, err
	}
	models, err := bedrockListFoundationModelIDs(ctx, cfg)
	if err != nil {
		return nil, canonical.BadEndpoint("bedrock AWS model catalog request failed")
	}
	return models, nil
}

func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		return &url.URL{}
	}
	return u
}

func applyBedrockAuth(ctx context.Context, credentialRef string, req *http.Request, payload []byte) error {
	mode, value := parseBedrockAuthMode(credentialRef)
	switch mode {
	case bedrockAuthModeAPIKeyEnv:
		if value == "" {
			value = "AWS_BEARER_TOKEN_BEDROCK"
		}
		token := trimBedrockInput(platformconfig.ReadEnvTrim(value))
		if token == "" {
			return canonical.BadEndpoint("bedrock API key env var is missing: " + value)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	case bedrockAuthModeAWSProfile:
		return signBedrockRequestWithAWSProfile(ctx, value, req, payload)
	default:
		return canonical.BadEndpoint("bedrock auth mode is unsupported")
	}
}

func signBedrockRequestWithAWSProfile(ctx context.Context, profile string, req *http.Request, payload []byte) error {
	region, err := bedrockSigningRegion(ctx, req.URL, profile)
	if err != nil {
		return err
	}
	cfg, err := loadBedrockAWSConfig(ctx, region, profile)
	if err != nil {
		return err
	}
	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		if trimBedrockInput(profile) != "" {
			return canonical.BadEndpoint(fmt.Sprintf("bedrock AWS credentials for profile %q are unavailable or expired", profile))
		}
		return canonical.BadEndpoint("bedrock AWS credentials are unavailable or expired")
	}
	signer := v4.NewSigner()
	return signer.SignHTTP(ctx, aws.Credentials{
		AccessKeyID:     creds.AccessKeyID,
		SecretAccessKey: creds.SecretAccessKey,
		SessionToken:    creds.SessionToken,
		Source:          creds.Source,
		CanExpire:       creds.CanExpire,
		Expires:         creds.Expires,
		AccountID:       creds.AccountID,
	}, req, sha256Hex(payload), bedrockSigningService, region, time.Now().UTC())
}

func loadBedrockAWSConfig(ctx context.Context, region, profile string) (aws.Config, error) {
	loadOptions := []func(*config.LoadOptions) error{config.WithRegion(region)}
	if trimBedrockInput(profile) != "" {
		loadOptions = append(loadOptions, config.WithSharedConfigProfile(trimBedrockInput(profile)))
	}
	cfg, err := config.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		if trimBedrockInput(profile) != "" {
			return aws.Config{}, canonical.BadEndpoint(fmt.Sprintf("bedrock AWS profile %q could not be loaded", profile))
		}
		return aws.Config{}, canonical.BadEndpoint("bedrock AWS default credential chain could not be loaded")
	}
	return cfg, nil
}

func parseBedrockAuthMode(credentialRef string) (mode bedrockAuthMode, value string) {
	ref := trimBedrockInput(credentialRef)
	if ref == "" || strings.EqualFold(ref, string(providercatalog.AuthVariantAWSProfile)) {
		return bedrockAuthModeAWSProfile, ""
	}
	if strings.HasPrefix(lowerBedrockInput(ref), "profile:") { // swobu:io-string source=boundary
		return bedrockAuthModeAWSProfile, trimBedrockInput(ref[len("profile:"):])
	}
	// Backward-compat: preserve previous bedrock_api_key_env spelling.
	if strings.EqualFold(ref, "bedrock_api_key_env") {
		return bedrockAuthModeAPIKeyEnv, "AWS_BEARER_TOKEN_BEDROCK"
	}
	if strings.EqualFold(ref, string(providercatalog.AuthVariantEnv)) {
		return bedrockAuthModeAPIKeyEnv, "AWS_BEARER_TOKEN_BEDROCK"
	}
	if strings.HasPrefix(lowerBedrockInput(ref), "env:") { // swobu:io-string source=boundary
		return bedrockAuthModeAPIKeyEnv, trimBedrockInput(ref[len("env:"):])
	}
	return bedrockAuthModeAWSProfile, ""
}

func bedrockSigningRegion(ctx context.Context, u *url.URL, profile string) (string, error) {
	if envRegion := trimBedrockInput(platformconfig.ReadEnvTrim("AWS_REGION")); envRegion != "" { // swobu:io-string source=boundary
		return envRegion, nil
	}
	if envRegion := trimBedrockInput(platformconfig.ReadEnvTrim("AWS_DEFAULT_REGION")); envRegion != "" { // swobu:io-string source=boundary
		return envRegion, nil
	}
	if sdkRegion := bedrockRegionFromSDKConfig(ctx, profile); sdkRegion != "" {
		return sdkRegion, nil
	}
	host := trimBedrockInput(u.Hostname())
	parts := strings.Split(host, ".")
	if len(parts) >= 4 && strings.HasPrefix(parts[0], "bedrock-runtime") {
		return parts[1], nil
	}
	// TODO(bedrock-auth-ontology): allow explicit region in durable provider config once auth ontology supports non-token credential parameters.
	return "", canonical.BadEndpoint("bedrock signing region is required (set AWS_REGION/AWS_DEFAULT_REGION or use a bedrock-runtime.<region> host)")
}

func bedrockRegionFromSDKConfig(ctx context.Context, profile string) string {
	opts := make([]func(*config.LoadOptions) error, 0, 1)
	if trimBedrockInput(profile) != "" {
		opts = append(opts, config.WithSharedConfigProfile(trimBedrockInput(profile)))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return ""
	}
	return trimBedrockInput(cfg.Region)
}

func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func trimBedrockInput(value string) string {
	return strings.TrimSpace(value) // swobu:io-string source=boundary
}

func lowerBedrockInput(value string) string {
	return strings.ToLower(value) // swobu:io-string source=boundary
}
