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

// ListDeployments reads the provider-selected deployment catalog for one
// target. This is an operator-support path, not request-path capability policy.
func (e BackendAdapter) ListDeployments(ctx context.Context, target provider.TargetSnapshot) ([]profile.ProviderDeploymentRecord, error) {
	if strings.TrimSpace(target.BaseURL) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("provider endpoint base URL is required")
	}
	if parsed, ok := profile.ParseProviderID(strings.TrimSpace(target.ProviderID())); !ok || parsed != e.profile.ProviderID() { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("provider policy is unsupported for model catalog target")
	}
	if requiresExplicitCredentialRef(target.ProviderID(), target.BaseURL, target.CredentialRef) {
		return nil, canonical.BadEndpoint(providerCredentialRequiredMessage(target.ProviderID()))
	}

	switch e.profile.ModelCatalogDialect() {
	case ModelCatalogOpenAI:
		return e.listOpenAIModels(ctx, target)
	case ModelCatalogLMStudioV1:
		return e.listLMStudioModels(ctx, target)
	default:
		return nil, canonical.InternalError("provider model catalog dialect is unspecified")
	}
}

func (e BackendAdapter) listLMStudioModels(ctx context.Context, target provider.TargetSnapshot) ([]profile.ProviderDeploymentRecord, error) {
	nativeURL, err := LMStudioNativeModelsURL(target.BaseURL)
	if err != nil {
		return nil, canonical.BadEndpoint(err.Error())
	}
	resp, err := e.getModelCatalog(ctx, target, nativeURL)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		_ = resp.Body.Close()
		return e.listOpenAIModels(ctx, target)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return nil, httpedge.ReadBackendHTTPError(resp, target.TargetID)
	}
	deployments, err := decodeLMStudioModels(resp.Body)
	if err != nil {
		return nil, canonical.InternalError("backend LM Studio model catalog could not be decoded")
	}
	return deployments, nil
}

func (e BackendAdapter) listOpenAIModels(ctx context.Context, target provider.TargetSnapshot) ([]profile.ProviderDeploymentRecord, error) {
	resp, err := e.getModelCatalog(ctx, target, httpedge.JoinBaseURLAndPath(target.BaseURL, "/models"))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return nil, httpedge.ReadBackendHTTPError(resp, target.TargetID)
	}
	models, err := modelcatalogopenai.DecodeModelIDs(resp.Body)
	if err != nil {
		return nil, canonical.InternalError("backend model catalog could not be decoded")
	}
	out := make([]profile.ProviderDeploymentRecord, 0, len(models))
	for _, modelID := range models {
		out = append(out, profile.NewProviderDeployment(modelID, modelID, target.ProviderID(), "", target.ProviderID(), nil, ""))
	}
	return out, nil
}

func (e BackendAdapter) getModelCatalog(ctx context.Context, target provider.TargetSnapshot, requestURL string) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, canonical.BadEndpoint("provider endpoint model catalog request could not be built")
	}
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)
	if err := e.applyCredential(ctx, httpReq, target.ProviderID(), target.CredentialRef, target.AuthHeader()); err != nil {
		return nil, err
	}
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, canonical.BadEndpoint("provider endpoint model catalog request failed before backend response")
	}
	decoded, err := httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		_ = resp.Body.Close()
		return nil, canonical.InternalError("backend response content encoding is unsupported or invalid")
	}
	return decoded, nil
}

func (e BackendAdapter) ProbeTarget(ctx context.Context, target provider.TargetSnapshot) (provider.TargetProbeResult, error) {
	deployments, err := e.ListDeployments(ctx, target)
	return provider.TargetProbeResult{Deployments: deployments}, err
}
