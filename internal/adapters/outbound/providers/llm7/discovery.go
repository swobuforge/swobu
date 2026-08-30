package llm7

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/httpedge"
	modelcatalogopenai "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/openai"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

type discovery struct {
	client      *http.Client
	credentials providersruntime.CredentialProvider
}

func (d discovery) ProbeTarget(ctx context.Context, target provider.TargetSnapshot) (provider.TargetProbeResult, error) {
	if strings.TrimSpace(target.BaseURL) == "" {
		return provider.TargetProbeResult{}, canonical.BadEndpoint("LLM7 endpoint base URL is required")
	}
	client := d.client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpedge.JoinBaseURLAndPath(target.BaseURL, "/models"), nil)
	if err != nil {
		return provider.TargetProbeResult{}, canonical.BadEndpoint("LLM7 catalog request could not be built")
	}
	req.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	req.Header.Set("User-Agent", "swobu/dev")
	if strings.TrimSpace(target.CredentialRef) != "" {
		if d.credentials == nil {
			return provider.TargetProbeResult{}, canonical.BadEndpoint("credential resolver is not configured")
		}
		token, resolveErr := d.credentials.ResolveCredential(ctx, target.ProviderID(), target.CredentialRef)
		if resolveErr != nil || strings.TrimSpace(token) == "" {
			return provider.TargetProbeResult{}, canonical.BadEndpoint("LLM7 credential reference could not be resolved")
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return provider.TargetProbeResult{}, provider.TransportFailure(ctx, err)
	}
	resp, err = httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		_ = resp.Body.Close()
		return provider.TargetProbeResult{}, canonical.InternalError("LLM7 catalog response content encoding is unsupported or invalid")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusBadRequest {
		return provider.TargetProbeResult{}, httpedge.ReadBackendHTTPError(resp, target.TargetID)
	}
	var payload json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return provider.TargetProbeResult{}, canonical.InternalError("LLM7 catalog response could not be decoded")
	}
	modelIDs, err := modelcatalogopenai.DecodeModelIDs(bytes.NewReader(payload))
	if err != nil || modelIDs == nil {
		// Historical LLM7 deployments returned a provider-specific top-level
		// array. Retain that exact public contract while the current documented
		// OpenAI list envelope remains the canonical decoding path.
		var records []struct {
			ID string `json:"id"`
		}
		if arrayErr := json.Unmarshal(payload, &records); arrayErr != nil || records == nil {
			return provider.TargetProbeResult{}, canonical.InternalError("LLM7 catalog response could not be decoded")
		}
		modelIDs = make([]string, 0, len(records))
		for _, record := range records {
			modelIDs = append(modelIDs, record.ID)
		}
	}
	ids := []string{"default", "fast", "pro"}
	seen := map[string]struct{}{"default": {}, "fast": {}, "pro": {}}
	for _, modelID := range modelIDs {
		id := strings.TrimSpace(modelID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Strings(ids[3:])
	deployments := make([]profile.ModelAuthoringOption, 0, len(ids))
	for _, id := range ids {
		deployments = append(deployments, profile.NewModelAuthoringOption(id, id, "LLM7", "", "", nil, ""))
	}
	return provider.TargetProbeResult{Options: deployments}, nil
}

var _ providersruntime.Discovery = discovery{}
