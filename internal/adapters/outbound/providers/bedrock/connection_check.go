package bedrock

import (
	"context"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/config"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

// CheckConnectionCatalog uses the supplied bounded client for both catalog and
// AWS credential-chain HTTP effects. It omits optional STS identity enrichment;
// a connectivity observation neither needs nor retains account identity.
func CheckConnectionCatalog(ctx context.Context, target provider.TargetSnapshot, client *http.Client, credentials providersruntime.CredentialProvider) (provider.TargetProbeResult, error) {
	e := NewExecutor(client)
	e.credentials = credentials
	region := target.BedrockRegion()
	var resolved resolvedBedrockAuthState
	if strings.TrimSpace(target.CredentialRef) != "" {
		var err error
		resolved, err = resolveBedrockAuth(ctx, e.credentials, target, region)
		if err != nil {
			return provider.TargetProbeResult{}, err
		}
	} else {
		cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithHTTPClient(e.client))
		if err != nil {
			return provider.TargetProbeResult{}, canonical.BadEndpoint("AWS credential configuration unavailable")
		}
		if err := boundSelectedProcess(&cfg); err != nil {
			return provider.TargetProbeResult{}, canonical.BadEndpoint("AWS credential source unavailable")
		}
		credentials, err := cfg.Credentials.Retrieve(ctx)
		if err != nil {
			return provider.TargetProbeResult{}, canonical.BadEndpoint("AWS credentials unavailable")
		}
		resolved = resolvedBedrockAuthState{kind: bedrockAuthAWSIdentity, credentials: &credentials}
	}
	options, err := e.listDeploymentsWithAuth(ctx, target, resolved, region)
	return provider.TargetProbeResult{Options: options}, err
}
