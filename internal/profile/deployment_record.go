package profile

import (
	"slices"
	"strings"
)

// ModelAuthoringOption carries one advisory catalog option plus the provider
// facts that the UI and probe surfaces can render without re-inferring policy.
type ModelAuthoringOption struct {
	Name                       string   `json:"name"`
	ModelName                  string   `json:"model_name,omitempty"`
	ModelPublisher             string   `json:"model_publisher,omitempty"`
	ModelVersion               string   `json:"model_version,omitempty"`
	Family                     string   `json:"family,omitempty"`
	SupportedProviderProtocols []string `json:"supported_provider_protocols,omitempty"`
	DefaultProviderProtocol    string   `json:"default_provider_protocol,omitempty"`
}

// NewModelAuthoringOption builds one immutable model-authoring option from raw
// catalog data.
func NewModelAuthoringOption(
	name string,
	modelName string,
	modelPublisher string,
	modelVersion string,
	family string,
	supportedProtocols []string,
	defaultProtocol string,
) ModelAuthoringOption {
	option := ModelAuthoringOption{
		Name:                       strings.TrimSpace(name),           // swobu:io-string source=boundary
		ModelName:                  strings.TrimSpace(modelName),      // swobu:io-string source=boundary
		ModelPublisher:             strings.TrimSpace(modelPublisher), // swobu:io-string source=boundary
		ModelVersion:               strings.TrimSpace(modelVersion),   // swobu:io-string source=boundary
		Family:                     strings.TrimSpace(family),         // swobu:io-string source=boundary
		SupportedProviderProtocols: CloneModelIDs(supportedProtocols),
		DefaultProviderProtocol:    strings.TrimSpace(defaultProtocol), // swobu:io-string source=boundary
	}
	return option
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

// CloneModelAuthoringOptions protects model-authoring options from accidental
// mutation by callers or transport renderers.
func CloneModelAuthoringOptions(options []ModelAuthoringOption) []ModelAuthoringOption {
	out := make([]ModelAuthoringOption, 0, len(options))
	for _, option := range options {
		option.Name = strings.TrimSpace(option.Name)                                       // swobu:io-string source=boundary
		option.ModelName = strings.TrimSpace(option.ModelName)                             // swobu:io-string source=boundary
		option.ModelPublisher = strings.TrimSpace(option.ModelPublisher)                   // swobu:io-string source=boundary
		option.ModelVersion = strings.TrimSpace(option.ModelVersion)                       // swobu:io-string source=boundary
		option.Family = strings.TrimSpace(option.Family)                                   // swobu:io-string source=boundary
		option.DefaultProviderProtocol = strings.TrimSpace(option.DefaultProviderProtocol) // swobu:io-string source=boundary
		option.SupportedProviderProtocols = CloneModelIDs(option.SupportedProviderProtocols)
		out = append(out, option)
	}
	return slices.Clone(out)
}
