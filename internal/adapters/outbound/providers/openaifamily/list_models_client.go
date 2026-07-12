package openaifamily

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/httpedge"
	modelcatalogopenai "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/openai"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
)

// ListModels reads the OpenAI-style model catalog for one selected
// provider target. This is an operator-support path.
func (e ProviderIngressResolverAdapter) ListModels(ctx context.Context, target exchange.RoutableTarget) ([]string, error) {
	if strings.TrimSpace(target.BaseURL) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("OpenAI-family provider base URL is required")
	}
	if requiresExplicitCredentialRef(target.ProviderID(), target.BaseURL, target.CredentialRef) {
		return nil, canonical.BadEndpoint(providerCredentialRequiredMessage(target.ProviderID()))
	}

	if projectEndpoint := azureProjectEndpointForModelCatalog(target.BaseURL, e.azureProjectEndpoint); projectEndpoint != "" {
		return e.listAzureProjectDeployments(ctx, target, projectEndpoint)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, httpedge.JoinBaseURLAndPath(target.BaseURL, "/models"), nil)
	if err != nil {
		return nil, canonical.BadEndpoint("OpenAI-family provider model catalog request could not be built")
	}
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)
	if err := e.applyCredential(ctx, httpReq, target.ProviderID(), target.CredentialRef, target.AuthHeader); err != nil {
		return nil, err
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, canonical.BadEndpoint("OpenAI-family provider model catalog request failed before backend response")
	}
	resp, err = httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		defer func() { _ = resp.Body.Close() }()
		return nil, canonical.InternalError("backend response content encoding is unsupported or invalid")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return nil, httpedge.ReadBackendHTTPError(resp, target.BackendRef)
	}
	models, err := modelcatalogopenai.DecodeModelIDs(resp.Body)
	if err != nil {
		return nil, canonical.InternalError("backend model catalog could not be decoded")
	}
	return models, nil
}

type azureProjectDeploymentsListResponse struct {
	Value []azureProjectDeploymentEntry `json:"value"`
}

type azureProjectDeploymentEntry struct {
	Name string `json:"name"`
}

func (e ProviderIngressResolverAdapter) listAzureProjectDeployments(ctx context.Context, target exchange.RoutableTarget, projectEndpoint string) ([]string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(projectEndpoint, "/")+"/deployments?api-version=v1", nil)
	if err != nil {
		return nil, canonical.BadEndpoint("Azure project deployment catalog request could not be built")
	}
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)
	if err := e.applyCredential(ctx, httpReq, target.ProviderID(), target.CredentialRef, target.AuthHeader); err != nil {
		return nil, err
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, canonical.BadEndpoint("Azure project deployment catalog request failed before backend response")
	}
	resp, err = httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		defer func() { _ = resp.Body.Close() }()
		return nil, canonical.InternalError("backend response content encoding is unsupported or invalid")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return nil, httpedge.ReadBackendHTTPError(resp, target.BackendRef)
	}
	var decoded azureProjectDeploymentsListResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, canonical.InternalError("Azure project deployment catalog could not be decoded")
	}
	models := make([]string, 0, len(decoded.Value))
	for _, deployment := range decoded.Value {
		name := strings.TrimSpace(deployment.Name) // swobu:io-string source=boundary
		if name == "" {
			continue
		}
		models = append(models, name)
	}
	return models, nil
}

func azureProjectEndpointForModelCatalog(baseURL string, projectEndpoint string) string {
	baseURL = strings.TrimSpace(baseURL) // swobu:io-string source=boundary
	if baseURL == "" {
		return ""
	}
	if !strings.Contains(strings.ToLower(baseURL), ".openai.azure.com/openai/v1") && !strings.Contains(strings.ToLower(baseURL), ".services.ai.azure.com/openai/v1") { // swobu:io-string source=domain
		return ""
	}
	projectEndpoint = strings.TrimSpace(projectEndpoint) // swobu:io-string source=boundary
	if projectEndpoint == "" {
		return ""
	}
	if _, err := url.Parse(projectEndpoint); err != nil {
		return ""
	}
	return projectEndpoint
}

func (e ProviderIngressResolverAdapter) ValidateCredentials(ctx context.Context, target exchange.RoutableTarget) error {
	_, err := e.ListModels(ctx, target)
	return err
}
