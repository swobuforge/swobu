package bedrock

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	smithybearer "github.com/aws/smithy-go/auth/bearer"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/profile"
)

type bedrockAuthMode string

const (
	bedrockAuthModeAWSProfile bedrockAuthMode = "aws_profile"
	bedrockAuthModeAPIKeyEnv  bedrockAuthMode = "api_key_env"
	bedrockSigningService     = "bedrock"
)

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
	// Credential refs may carry an explicit @region suffix so provider config
	// can override ambient env and host defaults without a separate auth mode.
	profileName, _ := splitBedrockProfileAndRegion(profile)
	cfg, err := loadBedrockAWSConfig(ctx, region, bedrockAuthModeAWSProfile, profileName)
	if err != nil {
		return err
	}
	creds, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		if trimBedrockInput(profileName) != "" {
			return canonical.BadEndpoint(fmt.Sprintf("bedrock AWS credentials for profile %q are unavailable or expired", profileName))
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

func loadBedrockAWSConfig(ctx context.Context, region string, mode bedrockAuthMode, value string) (aws.Config, error) {
	profileName, _ := splitBedrockProfileAndRegion(value)
	if mode == bedrockAuthModeAWSProfile {
		value = profileName
	}
	loadOptions := []func(*config.LoadOptions) error{config.WithRegion(region)}
	switch mode {
	case bedrockAuthModeAWSProfile:
		if trimBedrockInput(value) != "" {
			loadOptions = append(loadOptions, config.WithSharedConfigProfile(trimBedrockInput(value)))
		}
	case bedrockAuthModeAPIKeyEnv:
		envKey := value
		if envKey == "" {
			envKey = "AWS_BEARER_TOKEN_BEDROCK"
		}
		token := trimBedrockInput(platformconfig.ReadEnvTrim(envKey))
		if token == "" {
			return aws.Config{}, canonical.BadEndpoint("bedrock API key env var is missing: " + envKey)
		}
		return aws.Config{
			Region: region,
			BearerAuthTokenProvider: smithybearer.StaticTokenProvider{
				Token: smithybearer.Token{Value: token},
			},
		}, nil
	default:
		return aws.Config{}, canonical.BadEndpoint("bedrock auth mode is unsupported")
	}
	cfg, err := config.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil && mode == bedrockAuthModeAWSProfile && trimBedrockInput(value) != "" {
		sharedConfig, sharedCreds, ok := defaultAWSSharedFiles()
		if ok {
			fallbackOpts := append([]func(*config.LoadOptions) error{}, loadOptions...)
			fallbackOpts = append(fallbackOpts,
				config.WithSharedConfigFiles([]string{sharedConfig}),
				config.WithSharedCredentialsFiles([]string{sharedCreds}),
			)
			cfg, err = config.LoadDefaultConfig(ctx, fallbackOpts...)
		}
	}
	if err != nil {
		if trimBedrockInput(value) != "" {
			return aws.Config{}, canonical.BadEndpoint(fmt.Sprintf("bedrock AWS profile %q could not be loaded", value))
		}
		return aws.Config{}, canonical.BadEndpoint("bedrock AWS default credential chain could not be loaded")
	}
	if mode == bedrockAuthModeAWSProfile {
		// Bedrock Mantle execution for aws_profile/aws_env_session must use SigV4.
		// Some shared config chains can surface bearer providers (for example SSO),
		// which breaks local/http test endpoints and non-bearer Mantle flows.
		cfg.BearerAuthTokenProvider = nil
		if _, credErr := cfg.Credentials.Retrieve(ctx); credErr != nil {
			if trimBedrockInput(value) != "" {
				return aws.Config{}, canonical.BadEndpoint(fmt.Sprintf("bedrock AWS credentials for profile %q are unavailable or expired: %v", value, credErr))
			}
			return aws.Config{}, canonical.BadEndpoint(fmt.Sprintf("bedrock AWS credentials are unavailable or expired: %v", credErr))
		}
	}
	return cfg, nil
}

func parseBedrockAuthMode(credentialRef string) (mode bedrockAuthMode, value string) {
	ref := trimBedrockInput(credentialRef)
	if ref == "" || strings.EqualFold(ref, string(profile.AuthVariantAWSProfile)) {
		return bedrockAuthModeAWSProfile, ""
	}
	if strings.EqualFold(ref, string(profile.AuthVariantAWSEnvSession)) {
		return bedrockAuthModeAWSProfile, ""
	}
	if strings.HasPrefix(lowerBedrockInput(ref), "profile:") { // swobu:io-string source=boundary
		return bedrockAuthModeAWSProfile, trimBedrockInput(ref[len("profile:"):])
	}
	if strings.EqualFold(ref, string(profile.AuthVariantEnv)) {
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
	if _, hostRegion := bedrockEndpointClassAndRegion(u.String()); hostRegion != "" {
		return hostRegion, nil
	}
	profileName, explicitRegion := splitBedrockProfileAndRegion(profile)
	if explicitRegion != "" {
		return explicitRegion, nil
	}
	if sdkRegion := bedrockRegionFromSDKConfig(ctx, profileName); sdkRegion != "" {
		return sdkRegion, nil
	}
	return "", canonical.BadEndpoint("bedrock signing region is required (set AWS_REGION/AWS_DEFAULT_REGION or use a bedrock-mantle.<region> host)")
}

func bedrockRegionFromSDKConfig(ctx context.Context, profile string) string {
	opts := make([]func(*config.LoadOptions) error, 0, 1)
	if trimBedrockInput(profile) != "" {
		opts = append(opts, config.WithSharedConfigProfile(trimBedrockInput(profile)))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil && trimBedrockInput(profile) != "" {
		sharedConfig, sharedCreds, ok := defaultAWSSharedFiles()
		if ok {
			fallbackOpts := append([]func(*config.LoadOptions) error{}, opts...)
			fallbackOpts = append(fallbackOpts,
				config.WithSharedConfigFiles([]string{sharedConfig}),
				config.WithSharedCredentialsFiles([]string{sharedCreds}),
			)
			cfg, err = config.LoadDefaultConfig(ctx, fallbackOpts...)
		}
	}
	if err != nil {
		return ""
	}
	return trimBedrockInput(cfg.Region)
}

func splitBedrockProfileAndRegion(value string) (profile string, region string) {
	trimmed := trimBedrockInput(value)
	if trimmed == "" {
		return "", ""
	}
	profile, region, found := strings.Cut(trimmed, "@") // swobu:io-string source=boundary
	if !found {
		return trimmed, ""
	}
	return trimBedrockInput(profile), trimBedrockInput(region)
}

func defaultAWSSharedFiles() (configPath string, credentialsPath string, ok bool) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" { // swobu:io-string source=boundary
		return "", "", false
	}
	configPath = filepath.Join(home, ".aws", "config")
	credentialsPath = filepath.Join(home, ".aws", "credentials")
	if !fileExists(configPath) || !fileExists(credentialsPath) {
		return "", "", false
	}
	return configPath, credentialsPath, true
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
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
