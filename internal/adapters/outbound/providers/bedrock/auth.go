package bedrock

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

const bedrockSigningService = "bedrock"

func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		return &url.URL{}
	}
	return u
}

type resolvedBedrockAuthState struct {
	kind        bedrockAuthKind
	token       string
	credentials *aws.Credentials
	config      *aws.Config
}

type bedrockAuthKind uint8

const (
	bedrockAuthTargetAPIKey bedrockAuthKind = iota + 1
	bedrockAuthAWSIdentity
)

func (k bedrockAuthKind) String() string {
	switch k {
	case bedrockAuthTargetAPIKey:
		return "explicit_api_key"
	case bedrockAuthAWSIdentity:
		return "aws_identity"
	default:
		return ""
	}
}

// resolveBedrockAuth is the only Bedrock authentication selection operation.
// A credential reference selects bearer authentication. Its absence selects
// the AWS SDK chain; runtime environment inspection never changes strategy.
func resolveBedrockAuth(ctx context.Context, credentials providersruntime.CredentialProvider, credentialRef, region string) (resolvedBedrockAuthState, error) {
	if ref := strings.TrimSpace(credentialRef); ref != "" {
		if credentials == nil {
			return resolvedBedrockAuthState{}, canonical.BadEndpoint("bedrock API key credential resolver is unavailable")
		}
		token, err := credentials.ResolveCredential(ctx, "bedrock", ref)
		if err != nil || strings.TrimSpace(token) == "" {
			return resolvedBedrockAuthState{}, canonical.BadEndpoint("bedrock API key credential could not be resolved")
		}
		return resolvedBedrockAuthState{kind: bedrockAuthTargetAPIKey, token: strings.TrimSpace(token)}, nil
	}
	cfg, err := loadBedrockAmbientConfig(ctx, region)
	if err != nil {
		return resolvedBedrockAuthState{}, err
	}
	retrieved, err := cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return resolvedBedrockAuthState{}, canonical.BadEndpoint("bedrock AWS credentials are unavailable or expired")
	}
	static := aws.NewCredentialsCache(credentialsProvider{credentials: retrieved})
	cfg.Credentials = static
	return resolvedBedrockAuthState{kind: bedrockAuthAWSIdentity, credentials: &retrieved, config: &cfg}, nil
}

type credentialsProvider struct{ credentials aws.Credentials }

func (p credentialsProvider) Retrieve(context.Context) (aws.Credentials, error) {
	return p.credentials, nil
}

func applyBedrockAuth(ctx context.Context, credentials providersruntime.CredentialProvider, credentialRef string, req *http.Request, payload []byte, region string) error {
	resolved, err := resolveBedrockAuth(ctx, credentials, credentialRef, region)
	if err != nil {
		return err
	}
	return applyResolvedBedrockAuth(ctx, resolved, req, payload, region)
}

func applyResolvedBedrockAuth(ctx context.Context, resolved resolvedBedrockAuthState, req *http.Request, payload []byte, region string) error {
	if resolved.token != "" {
		req.Header.Set("Authorization", "Bearer "+resolved.token)
		return nil
	}
	if resolved.credentials == nil {
		return canonical.BadEndpoint("bedrock authentication is unavailable")
	}
	creds := *resolved.credentials
	return v4.NewSigner().SignHTTP(ctx, aws.Credentials{
		AccessKeyID: creds.AccessKeyID, SecretAccessKey: creds.SecretAccessKey,
		SessionToken: creds.SessionToken, Source: creds.Source, CanExpire: creds.CanExpire,
		Expires: creds.Expires, AccountID: creds.AccountID,
	}, req, sha256Hex(payload), bedrockSigningService, region, time.Now().UTC())
}

// loadBedrockAmbientConfig always reloads the SDK chain so refresh observes
// changed shared config, SSO caches, and credential-process output.
func loadBedrockAmbientConfig(ctx context.Context, region string) (aws.Config, error) {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return aws.Config{}, canonical.BadEndpoint("bedrock AWS default credential chain could not be loaded")
	}
	return cfg, nil
}

// bedrockSigningRegionForTarget is the signing region for a target, drawn from
// the durable BedrockRegion fact rather than parsed from the endpoint host. The
// endpoint host never owns the signing region once region is first-class.
func bedrockSigningRegionForTarget(target provider.TargetSnapshot) string {
	return target.BedrockRegion()
}

// bedrockHostRegion returns the region implied by a recognized AWS Mantle host
// (canonical or PrivateLink), or "" for an unrecognized/custom host. Used by the
// validator to enforce region↔host consistency only for known AWS hosts.
func bedrockHostRegion(baseURL string) string {
	_, region := bedrockEndpointClassAndRegion(baseURL)
	return region
}

func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func trimBedrockInput(value string) string {
	return strings.TrimSpace(value) // swobu:io-string source=boundary
}
