package together

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/httpedge"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

// discovery is an operator-only convenience: its two independent source
// calls never select inference behavior or turn endpoint state into policy.
type discovery struct {
	client      *http.Client
	credentials providersruntime.CredentialProvider
}

func newDiscovery(client *http.Client, credentials providersruntime.CredentialProvider) discovery {
	if client == nil {
		client = http.DefaultClient
	}
	return discovery{client: client, credentials: credentials}
}

func (d discovery) ProbeTarget(ctx context.Context, target provider.TargetSnapshot) (provider.TargetProbeResult, error) {
	serverless, modelsErr := d.serverlessModels(ctx, target)
	dedicated, endpointsErr := d.dedicatedEndpoints(ctx, target)
	if modelsErr != nil && endpointsErr != nil {
		return provider.TargetProbeResult{}, modelsErr
	}
	return provider.TargetProbeResult{Options: mergeDeployments(dedicated, serverless)}, nil
}

func (d discovery) serverlessModels(ctx context.Context, target provider.TargetSnapshot) ([]profile.ModelAuthoringOption, error) {
	var payload struct {
		Data []struct {
			ID           string `json:"id"`
			Type         string `json:"type"`
			DisplayName  string `json:"display_name"`
			Organization string `json:"organization"`
		} `json:"data"`
	}
	if err := d.get(ctx, target, "/models", &payload); err != nil {
		return nil, err
	}
	deployments := make([]profile.ModelAuthoringOption, 0, len(payload.Data))
	for _, model := range payload.Data {
		if !togetherTextModelType(model.Type) {
			continue
		}
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		deployments = append(deployments, profile.NewModelAuthoringOption(id, model.DisplayName, model.Organization, "", model.Type, nil, ""))
	}
	return uniqueSorted(deployments), nil
}

func togetherTextModelType(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "chat", "language", "code":
		return true
	default:
		return false
	}
}

func (d discovery) dedicatedEndpoints(ctx context.Context, target provider.TargetSnapshot) ([]profile.ModelAuthoringOption, error) {
	var payload struct {
		Data []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"data"`
	}
	if err := d.get(ctx, target, "/endpoints?type=dedicated&mine=true", &payload); err != nil {
		return nil, err
	}
	deployments := make([]profile.ModelAuthoringOption, 0, len(payload.Data))
	for _, endpoint := range payload.Data {
		name := strings.TrimSpace(endpoint.Name)
		if name == "" {
			continue
		}
		deployments = append(deployments, profile.NewModelAuthoringOption(name, endpoint.Model, "Together AI", "", "dedicated", nil, ""))
	}
	return uniqueSorted(deployments), nil
}

func (d discovery) get(ctx context.Context, target provider.TargetSnapshot, requestPath string, output any) error {
	if strings.TrimSpace(target.BaseURL) == "" {
		return canonical.BadEndpoint("Together AI endpoint base URL is required")
	}
	if strings.TrimSpace(target.CredentialRef) == "" || d.credentials == nil {
		return canonical.BadEndpoint("together provider credential reference is required")
	}
	token, err := d.credentials.ResolveCredential(ctx, target.ProviderID(), target.CredentialRef)
	if err != nil || strings.TrimSpace(token) == "" {
		return canonical.BadEndpoint("Together AI credential reference could not be resolved")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpedge.JoinBaseURLAndPath(target.BaseURL, requestPath), nil)
	if err != nil {
		return canonical.BadEndpoint("Together AI catalog request could not be built")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	resp, err := d.client.Do(req)
	if err != nil {
		return canonical.BadEndpoint("Together AI catalog request failed before backend response")
	}
	resp, err = httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		_ = resp.Body.Close()
		return canonical.InternalError("Together AI catalog response content encoding is unsupported or invalid")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusBadRequest {
		return httpedge.ReadBackendHTTPError(resp, target.TargetID)
	}
	if err := json.NewDecoder(resp.Body).Decode(output); err != nil {
		return canonical.InternalError("Together AI catalog response could not be decoded")
	}
	return nil
}

func mergeDeployments(dedicated, serverless []profile.ModelAuthoringOption) []profile.ModelAuthoringOption {
	merged := append(uniqueSorted(dedicated), uniqueSorted(serverless)...)
	seen := make(map[string]struct{}, len(merged))
	out := make([]profile.ModelAuthoringOption, 0, len(merged))
	for _, deployment := range merged {
		if _, exists := seen[deployment.Name]; exists {
			continue
		}
		seen[deployment.Name] = struct{}{}
		out = append(out, deployment)
	}
	return out
}

func uniqueSorted(deployments []profile.ModelAuthoringOption) []profile.ModelAuthoringOption {
	byName := make(map[string]profile.ModelAuthoringOption, len(deployments))
	for _, deployment := range deployments {
		if deployment.Name != "" {
			byName[deployment.Name] = deployment
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]profile.ModelAuthoringOption, 0, len(names))
	for _, name := range names {
		out = append(out, byName[name])
	}
	return out
}

var _ providersruntime.Discovery = discovery{}
