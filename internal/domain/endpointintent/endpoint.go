package endpointintent

import (
	"fmt"
	"slices"
	"strings"
)

type Endpoint struct {
	name                      EndpointName
	providerConfigs           []ProviderConfig
	selectedProviderConfigRef ProviderConfigRef
}

// NewEndpoint enforces durable endpoint intent invariants at construction time.
// Request-time routing guesses are forbidden, so the selected provider config
// must already be explicit here.
func NewEndpoint(
	name EndpointName,
	providerConfigs []ProviderConfig,
	selectedProviderConfigRef ProviderConfigRef,
) (Endpoint, error) {
	if name.IsZero() {
		return Endpoint{}, fmt.Errorf("%w: endpoint name is required", ErrInvalidEndpoint)
	}
	if len(providerConfigs) == 0 {
		return Endpoint{}, fmt.Errorf("%w: endpoint must have at least one provider config", ErrInvalidEndpoint)
	}
	if selectedProviderConfigRef.value == "" {
		return Endpoint{}, fmt.Errorf("%w: selected provider config ref is required", ErrInvalidEndpoint)
	}
	configs := slices.Clone(providerConfigs)
	seen := make(map[string]struct{}, len(configs))
	seenAlias := make(map[string]struct{}, len(configs))
	seenProviderModelLiteral := make(map[string]struct{}, len(configs))
	seenModelID := make(map[string]struct{}, len(configs))
	selectedFound := false
	for _, providerConfig := range configs {
		ref := providerConfig.Ref().String()
		if ref == "" {
			return Endpoint{}, fmt.Errorf("%w: provider config ref is required", ErrInvalidEndpoint)
		}
		if _, exists := seen[ref]; exists {
			return Endpoint{}, fmt.Errorf("%w: provider config ref must be unique", ErrInvalidEndpoint)
		}
		seen[ref] = struct{}{}
		providerModelLiteral := strings.TrimSpace(providerConfig.ProviderSpec().String()) + ":" + strings.TrimSpace(providerConfig.ModelID()) // trimlowerlint:allow domain canonicalization
		providerModelLiteral = strings.ToLower(strings.TrimSpace(providerModelLiteral))                                                       // trimlowerlint:allow domain canonicalization
		if providerModelLiteral != ":" {
			seenProviderModelLiteral[providerModelLiteral] = struct{}{}
		}
		modelID := strings.ToLower(strings.TrimSpace(providerConfig.ModelID())) // trimlowerlint:allow domain canonicalization
		if modelID != "" {
			seenModelID[modelID] = struct{}{}
		}
		alias := strings.ToLower(strings.TrimSpace(providerConfig.TargetAlias())) // trimlowerlint:allow domain canonicalization
		if alias != "" {
			if _, exists := seenAlias[alias]; exists {
				return Endpoint{}, fmt.Errorf("%w: target alias must be unique per endpoint", ErrInvalidEndpoint)
			}
			seenAlias[alias] = struct{}{}
		}
		if providerConfig.Ref() == selectedProviderConfigRef {
			selectedFound = true
		}
	}
	for alias := range seenAlias {
		if _, exists := seenProviderModelLiteral[alias]; exists {
			return Endpoint{}, fmt.Errorf("%w: target alias must not collide with provider:model selectors", ErrInvalidEndpoint)
		}
		if _, exists := seenModelID[alias]; exists {
			return Endpoint{}, fmt.Errorf("%w: target alias must not collide with model selectors", ErrInvalidEndpoint)
		}
	}
	if !selectedFound {
		return Endpoint{}, fmt.Errorf("%w: selected provider config must resolve to one provider config", ErrInvalidEndpoint)
	}

	return Endpoint{
		name:                      name,
		providerConfigs:           configs,
		selectedProviderConfigRef: selectedProviderConfigRef,
	}, nil
}

func (e Endpoint) Name() EndpointName {
	return e.name
}

func (e Endpoint) ProviderConfigs() []ProviderConfig {
	return slices.Clone(e.providerConfigs)
}

// SelectedProviderConfig returns the one explicitly selected provider config
// guaranteed by the endpoint invariant.
func (e Endpoint) SelectedProviderConfig() ProviderConfig {
	for _, providerConfig := range e.providerConfigs {
		if providerConfig.Ref() == e.selectedProviderConfigRef {
			return providerConfig
		}
	}
	return ProviderConfig{}
}

func (e Endpoint) SelectedProviderConfigRef() ProviderConfigRef {
	return e.selectedProviderConfigRef
}
