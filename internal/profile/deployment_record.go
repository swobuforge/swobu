package profile

import (
	"slices"
	"strings"
)

// ProviderDeploymentRecord carries one catalog-visible deployment plus the discovery
// facts the UI and probe surfaces can render without re-inferring provider
// policy.
type ProviderDeploymentRecord struct {
	Name                       string   `json:"name"`
	ModelName                  string   `json:"model_name,omitempty"`
	ModelPublisher             string   `json:"model_publisher,omitempty"`
	ModelVersion               string   `json:"model_version,omitempty"`
	Family                     string   `json:"family,omitempty"`
	SupportedProviderProtocols []string `json:"supported_provider_protocols,omitempty"`
	DefaultProviderProtocol    string   `json:"default_provider_protocol,omitempty"`
}

// NewProviderDeployment builds one immutable deployment descriptor from raw
// catalog data.
func NewProviderDeployment(
	name string,
	modelName string,
	modelPublisher string,
	modelVersion string,
	family string,
	supportedProtocols []string,
	defaultProtocol string,
) ProviderDeploymentRecord {
	deployment := ProviderDeploymentRecord{
		Name:                       strings.TrimSpace(name),           // swobu:io-string source=boundary
		ModelName:                  strings.TrimSpace(modelName),      // swobu:io-string source=boundary
		ModelPublisher:             strings.TrimSpace(modelPublisher), // swobu:io-string source=boundary
		ModelVersion:               strings.TrimSpace(modelVersion),   // swobu:io-string source=boundary
		Family:                     strings.TrimSpace(family),         // swobu:io-string source=boundary
		SupportedProviderProtocols: CloneModelIDs(supportedProtocols),
		DefaultProviderProtocol:    strings.TrimSpace(defaultProtocol), // swobu:io-string source=boundary
	}
	return deployment
}

// CloneModelIDs protects operator read models from accidental mutation by
// callers or transport renderers.
func CloneModelIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id) // swobu:io-string source=boundary
		if id == "" {
			continue
		}
		out = append(out, id)
	}
	return slices.Clone(out)
}

// CloneProviderDeployments protects deployment descriptors from accidental
// mutation by callers or transport renderers.
func CloneProviderDeployments(deployments []ProviderDeploymentRecord) []ProviderDeploymentRecord {
	out := make([]ProviderDeploymentRecord, 0, len(deployments))
	for _, deployment := range deployments {
		deployment.Name = strings.TrimSpace(deployment.Name)                                       // swobu:io-string source=boundary
		deployment.ModelName = strings.TrimSpace(deployment.ModelName)                             // swobu:io-string source=boundary
		deployment.ModelPublisher = strings.TrimSpace(deployment.ModelPublisher)                   // swobu:io-string source=boundary
		deployment.ModelVersion = strings.TrimSpace(deployment.ModelVersion)                       // swobu:io-string source=boundary
		deployment.Family = strings.TrimSpace(deployment.Family)                                   // swobu:io-string source=boundary
		deployment.DefaultProviderProtocol = strings.TrimSpace(deployment.DefaultProviderProtocol) // swobu:io-string source=boundary
		deployment.SupportedProviderProtocols = CloneModelIDs(deployment.SupportedProviderProtocols)
		out = append(out, deployment)
	}
	return slices.Clone(out)
}
