package openaifamily

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/httpedge"
	modelcatalogopenai "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/openai"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

// ListDeployments reads the provider-selected deployment catalog for one
// target. This is an operator-support path, not request-path capability policy.
func (e BackendAdapter) ListDeployments(ctx context.Context, target provider.TargetSnapshot) ([]profile.ModelAuthoringOption, error) {
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

func (e BackendAdapter) listLMStudioModels(ctx context.Context, target provider.TargetSnapshot) ([]profile.ModelAuthoringOption, error) {
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

func (e BackendAdapter) listOpenAIModels(ctx context.Context, target provider.TargetSnapshot) ([]profile.ModelAuthoringOption, error) {
	requestURL := httpedge.JoinBaseURLAndPath(target.BaseURL, "/models")
	requestURL, err := e.profile.ModelCatalogPolicy().decorateURL(requestURL)
	if err != nil {
		return nil, canonical.BadEndpoint("provider endpoint model catalog URL is invalid")
	}
	resp, err := e.getModelCatalog(ctx, target, requestURL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	// An enumerable provider's catalog is advisory: Cockpit keeps exact model
	// entry open. A valid inference front door may omit /models, so only the
	// route policy's explicit absence statuses become an empty catalog.
	if e.profile.ModelCatalogPolicy().missingStatus(resp.StatusCode) {
		return []profile.ModelAuthoringOption{}, nil
	}
	if resp.StatusCode >= 400 {
		return nil, httpedge.ReadBackendHTTPError(resp, target.TargetID)
	}
	rows, err := modelcatalogopenai.DecodeModelRows(resp.Body)
	if err != nil {
		return nil, canonical.InternalError("backend model catalog could not be decoded")
	}
	out := make([]profile.ModelAuthoringOption, 0, len(rows))
	for _, row := range rows {
		deployment, include, projectErr := e.profile.ModelCatalogPolicy().projectRow(e.profile.ProviderID(), row)
		if projectErr != nil {
			return nil, canonical.InternalError("backend model catalog row could not be projected")
		}
		if include {
			out = append(out, deployment)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	unique := out[:0]
	for _, deployment := range out {
		if len(unique) > 0 && unique[len(unique)-1].Name == deployment.Name {
			continue
		}
		unique = append(unique, deployment)
	}
	return unique, nil
}

func (e BackendAdapter) getModelCatalog(ctx context.Context, target provider.TargetSnapshot, requestURL string) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, canonical.BadEndpoint("provider endpoint model catalog request could not be built")
	}
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)
	// Model catalog reads are an OpenAI-family endpoint, independent of the
	// target's selected inference protocol and its protocol-only headers.
	if err := e.applyCredential(ctx, httpReq, target.ProviderID(), target.CredentialRef, target.AuthHeader(), protocolkind.ChatCompletions); err != nil {
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
	return provider.TargetProbeResult{Options: deployments}, err
}
