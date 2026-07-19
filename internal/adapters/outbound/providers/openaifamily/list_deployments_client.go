package openaifamily

import (
	"context"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/httpedge"
	modelcatalogopenai "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/openai"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

// ListDeployments reads the OpenAI-style deployment catalog for one selected
// provider target. This is an operator-support path.
func (e BackendAdapter) ListDeployments(ctx context.Context, target provider.TargetSnapshot) ([]profile.ProviderDeploymentRecord, error) {
	if strings.TrimSpace(target.BaseURL) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("provider endpoint base URL is required")
	}
	if requiresExplicitCredentialRef(target.ProviderID(), target.BaseURL, target.CredentialRef) {
		return nil, canonical.BadEndpoint(providerCredentialRequiredMessage(target.ProviderID()))
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, httpedge.JoinBaseURLAndPath(target.BaseURL, "/models"), nil)
	if err != nil {
		return nil, canonical.BadEndpoint("provider endpoint model catalog request could not be built")
	}
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)
	if err := e.applyCredential(ctx, httpReq, target.ProviderID(), target.CredentialRef, target.AuthHeader); err != nil {
		return nil, err
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, canonical.BadEndpoint("provider endpoint model catalog request failed before backend response")
	}
	resp, err = httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		defer func() { _ = resp.Body.Close() }()
		return nil, canonical.InternalError("backend response content encoding is unsupported or invalid")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return nil, httpedge.ReadBackendHTTPError(resp, target.TargetID)
	}
	models, err := modelcatalogopenai.DecodeModelIDs(resp.Body)
	if err != nil {
		return nil, canonical.InternalError("backend model catalog could not be decoded")
	}
	supportedProtocols := profile.ConcreteProviderProtocolsForSpec(target.ProviderID())
	out := make([]profile.ProviderDeploymentRecord, 0, len(models))
	for _, modelID := range models {
		out = append(out, profile.NewProviderDeployment(
			modelID,
			modelID,
			target.ProviderID(),
			"",
			target.ProviderID(),
			supportedProtocols,
			"",
		))
	}
	return out, nil
}

func (e BackendAdapter) ProbeTarget(ctx context.Context, target provider.TargetSnapshot) (provider.TargetProbeResult, error) {
	deployments, err := e.ListDeployments(ctx, target)
	return provider.TargetProbeResult{Deployments: deployments}, err
}
