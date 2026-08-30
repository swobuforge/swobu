package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/httpedge"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

// discovery uses the Gemini Models endpoint solely to offer operator model
// choices. Its v1beta path is not inference target state and catalog fields do
// not establish request capabilities.
type discovery struct{ runtime *geminiRuntime }

func (d discovery) ProbeTarget(ctx context.Context, target provider.TargetSnapshot) (provider.TargetProbeResult, error) {
	if err := validateTarget(target); err != nil {
		return provider.TargetProbeResult{}, err
	}
	auth, err := d.runtime.resolveAuth(ctx, target)
	if err != nil {
		return provider.TargetProbeResult{}, err
	}
	endpoint, err := geminiModelsURL(target.BaseURL)
	if err != nil {
		return provider.TargetProbeResult{}, err
	}
	var deployments []profile.ModelAuthoringOption
	pageToken := ""
	for {
		requestURL := endpoint
		if pageToken != "" {
			parsed, parseErr := url.Parse(requestURL)
			if parseErr != nil {
				return provider.TargetProbeResult{}, canonical.BadEndpoint("Gemini provider model catalog URL is invalid")
			}
			query := parsed.Query()
			query.Set("pageToken", pageToken)
			parsed.RawQuery = query.Encode()
			requestURL = parsed.String()
		}
		httpRequest, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
		if requestErr != nil {
			return provider.TargetProbeResult{}, canonical.BadEndpoint("Gemini provider model catalog request could not be built")
		}
		httpRequest.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
		applyAuth(httpRequest, auth)
		response, requestErr := d.runtime.client.Do(httpRequest)
		if requestErr != nil {
			return provider.TargetProbeResult{}, provider.TransportFailure(ctx, requestErr)
		}
		response, requestErr = httpedge.DecodeHTTPResponseContentEncoding(response)
		if requestErr != nil {
			defer func() { _ = response.Body.Close() }()
			return provider.TargetProbeResult{}, canonical.InternalError("backend model catalog response content encoding is unsupported or invalid")
		}
		if response.StatusCode >= http.StatusBadRequest {
			defer func() { _ = response.Body.Close() }()
			return provider.TargetProbeResult{}, httpedge.ReadBackendHTTPError(response, target.TargetID)
		}
		var page geminiModelsPage
		decodeErr := json.NewDecoder(response.Body).Decode(&page)
		_ = response.Body.Close()
		if decodeErr != nil {
			return provider.TargetProbeResult{}, canonical.InternalError("Gemini provider model catalog could not be decoded")
		}
		for _, model := range page.Models {
			modelID := strings.TrimSpace(model.BaseModelID) // swobu:io-string source=provider-wire
			if modelID == "" {
				modelID = geminiModelIDFromResourceName(model.Name)
			}
			if modelID == "" {
				continue
			}
			deployments = append(deployments, profile.NewModelAuthoringOption(
				modelID, modelID, string(profile.ProviderSpecGemini), "", string(profile.ProviderSpecGemini),
				profile.ConcreteProviderProtocolsForSpec(string(profile.ProviderSpecGemini)), "",
			))
		}
		pageToken = strings.TrimSpace(page.NextPageToken) // swobu:io-string source=provider-wire
		if pageToken == "" {
			break
		}
	}
	return provider.TargetProbeResult{Options: deployments}, nil
}

type geminiModelsPage struct {
	Models []struct {
		Name        string `json:"name"`
		BaseModelID string `json:"baseModelId"`
		DisplayName string `json:"displayName"`
	} `json:"models"`
	NextPageToken string `json:"nextPageToken"`
}

// geminiModelIDFromResourceName projects the Models API's canonical
// "models/{id}" resource identity into the Interactions model field. This is
// protocol-field normalization, not model-family capability inference.
func geminiModelIDFromResourceName(name string) string {
	const prefix = "models/"
	name = strings.TrimSpace(name) // swobu:io-string source=provider-wire
	if !strings.HasPrefix(name, prefix) {
		return ""
	}
	modelID := strings.TrimSpace(strings.TrimPrefix(name, prefix))
	if modelID == "" || strings.Contains(modelID, "/") {
		return ""
	}
	return modelID
}

func geminiModelsURL(executeBaseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(executeBaseURL)) // swobu:io-string source=boundary
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || strings.TrimRight(parsed.Path, "/") != "/v1" {
		return "", canonical.BadEndpoint("Gemini provider base URL must end in /v1")
	}
	parsed.Path = "/v1beta/models"
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

var _ provider.Discovery = discovery{}
