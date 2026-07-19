package bedrock

import (
	"encoding/json"
	"log/slog"
	"net/url"
	"path"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

func logBedrockBackendDiagnostic(operation string, target provider.TargetSnapshot, requestPath string, payload []byte, backendErr canonical.BackendError) {
	msg := strings.TrimSpace(backendErr.Message) // swobu:io-string source=boundary
	if msg == "" {
		return
	}
	endpointClass, region := bedrockEndpointClassAndRegion(target.BaseURL)
	modelID := bedrockModelIDFromPayload(payload)
	foundationModelARN, inferenceProfileARN := bedrockModelARNCandidates(region, modelID)
	if requestPath != "" {
		operation = path.Clean(strings.TrimSpace(requestPath)) // swobu:io-string source=boundary
	}
	lower := strings.ToLower(msg) // swobu:io-string source=boundary
	switch {
	case strings.Contains(lower, "operation not allowed"):
		slog.Debug(
			"bedrock backend rejected operation",
			"operation", operation,
			"endpoint_class", endpointClass,
			"region", region,
			"base_url", strings.TrimSpace(target.BaseURL), // swobu:io-string source=boundary
			"model_id", modelID,
			"foundation_model_arn", foundationModelARN,
			"inference_profile_arn", inferenceProfileARN,
			"status_code", backendErr.StatusCode,
			"target_id", backendErr.TargetID,
			"diagnostic", "model/operation is not invokable for current account+region+api path",
		)
	case strings.Contains(lower, "does not exist") && strings.Contains(lower, "model"):
		slog.Debug(
			"bedrock backend model missing on endpoint",
			"operation", operation,
			"endpoint_class", endpointClass,
			"region", region,
			"base_url", strings.TrimSpace(target.BaseURL), // swobu:io-string source=boundary
			"model_id", modelID,
			"foundation_model_arn", foundationModelARN,
			"inference_profile_arn", inferenceProfileARN,
			"status_code", backendErr.StatusCode,
			"target_id", backendErr.TargetID,
			"diagnostic", "selected model id is unavailable on this endpoint/account/region",
		)
	}
}

func bedrockModelIDFromPayload(payload []byte) string {
	if len(payload) == 0 {
		return ""
	}
	var parsed struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Model) // swobu:io-string source=boundary
}

func bedrockEndpointClassAndRegion(baseURL string) (class string, region string) {
	class = "unknown"
	trimmed := strings.TrimSpace(baseURL) // swobu:io-string source=boundary
	if trimmed == "" {
		return class, ""
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return class, ""
	}
	host := strings.TrimSpace(strings.ToLower(u.Hostname())) // swobu:io-string source=boundary
	parts := strings.Split(host, ".")
	if len(parts) < 4 {
		return class, ""
	}
	switch {
	case strings.HasPrefix(parts[0], "bedrock-mantle") && parts[2] == "api" && parts[3] == "aws":
		class = "bedrock_mantle_openai_compat"
		region = strings.TrimSpace(parts[1]) // swobu:io-string source=boundary
	}
	return class, region
}

func bedrockModelARNCandidates(region string, modelID string) (foundationModelARN string, inferenceProfileARN string) {
	region = strings.TrimSpace(region)   // swobu:io-string source=boundary
	modelID = strings.TrimSpace(modelID) // swobu:io-string source=boundary
	if region == "" || modelID == "" {
		return "", ""
	}
	return "arn:aws:bedrock:" + region + "::foundation-model/" + modelID,
		"arn:aws:bedrock:" + region + "::inference-profile/" + modelID
}
