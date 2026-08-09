package openaifamily

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/swobuforge/swobu/internal/profile"
)

// LMStudioNativeModelsURL derives the native catalog URL from the configured
// OpenAI-compatible execution base without discarding a reverse-proxy prefix.
func LMStudioNativeModelsURL(executionBaseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(executionBaseURL)) // swobu:io-string source=boundary
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("LM Studio execution base URL is invalid")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("LM Studio execution base URL must not contain a query or fragment")
	}

	cleaned := strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(cleaned, "/v1") {
		return "", fmt.Errorf("LM Studio execution base URL must end in /v1")
	}
	cleaned = strings.TrimSuffix(cleaned, "/v1")
	parsed.Path = path.Join(cleaned, "/api/v1/models")
	parsed.RawPath = ""
	return parsed.String(), nil
}

func decodeLMStudioModels(body io.Reader) ([]profile.ProviderDeploymentRecord, error) {
	var payload struct {
		Models []struct {
			Type         string `json:"type"`
			Key          string `json:"key"`
			DisplayName  string `json:"display_name"`
			Publisher    string `json:"publisher"`
			Architecture string `json:"architecture"`
		} `json:"models"`
	}
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		return nil, err
	}

	byKey := make(map[string]profile.ProviderDeploymentRecord, len(payload.Models))
	for _, model := range payload.Models {
		key := strings.TrimSpace(model.Key)                                        // swobu:io-string source=boundary
		if !strings.EqualFold(strings.TrimSpace(model.Type), "llm") || key == "" { // swobu:io-string source=boundary
			continue
		}
		byKey[key] = profile.NewProviderDeployment(
			key,
			model.DisplayName,
			model.Publisher,
			"",
			model.Architecture,
			nil,
			"",
		)
	}

	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	out := make([]profile.ProviderDeploymentRecord, 0, len(keys))
	for _, key := range keys {
		out = append(out, byKey[key])
	}
	return out, nil
}
