package deepseek

import (
	"context"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/httpedge"
	modelcatalogopenai "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/openai"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/anthropic"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

const modelCatalogURL = "https://api.deepseek.com/models"

type Discovery struct {
	client      *http.Client
	credentials providersruntime.CredentialProvider
	catalogURL  string
}

func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	if client == nil {
		client = http.DefaultClient
	}
	return providersruntime.ProviderRuntimeBundle{
		ProviderID:         profile.ProviderSpecDeepSeek,
		BackendResolver:    anthropic.NewBackendAdapter(profile.ProviderSpecDeepSeek, client, credentials),
		CredentialProvider: credentials,
		Discovery: Discovery{
			client:      client,
			credentials: credentials,
			catalogURL:  modelCatalogURL,
		},
	}
}

func (d Discovery) ListDeployments(ctx context.Context, target provider.TargetSnapshot) ([]profile.ProviderDeploymentRecord, error) {
	if target.ProviderID() != string(profile.ProviderSpecDeepSeek) {
		return nil, canonical.BadEndpoint("selected provider does not match DeepSeek discovery")
	}
	if strings.TrimSpace(target.CredentialRef) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("DeepSeek provider credential reference is required")
	}
	if d.credentials == nil {
		return nil, canonical.BadEndpoint("credential resolver is not configured")
	}
	token, err := d.credentials.ResolveCredential(ctx, target.ProviderID(), target.CredentialRef)
	if err != nil {
		return nil, canonical.BadEndpoint("credential reference could not be resolved")
	}
	if strings.TrimSpace(token) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("credential reference resolved to an empty token")
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, d.catalogURL, nil)
	if err != nil {
		return nil, canonical.BadEndpoint("DeepSeek provider model catalog request could not be built")
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	resp, err := d.client.Do(httpReq)
	if err != nil {
		return nil, canonical.BadEndpoint("DeepSeek provider model catalog request failed before backend response")
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
	protocols := profile.ConcreteProviderProtocolsForSpec(string(profile.ProviderSpecDeepSeek))
	deployments := make([]profile.ProviderDeploymentRecord, 0, len(models))
	for _, modelID := range models {
		deployments = append(deployments, profile.NewProviderDeployment(
			modelID,
			modelID,
			string(profile.ProviderSpecDeepSeek),
			"",
			string(profile.ProviderSpecDeepSeek),
			protocols,
			"",
		))
	}
	return deployments, nil
}

func (d Discovery) ProbeTarget(ctx context.Context, target provider.TargetSnapshot) (provider.TargetProbeResult, error) {
	deployments, err := d.ListDeployments(ctx, target)
	return provider.TargetProbeResult{Deployments: deployments}, err
}

var _ provider.Discovery = Discovery{}
