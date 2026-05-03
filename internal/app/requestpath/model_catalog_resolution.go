package requestpath

import (
	"sort"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
)

type endpointModelCatalog struct {
	Entries         []endpointModelEntry
	ByAlias         map[string]endpointModelEntry
	ByMechanical    map[string]endpointModelEntry
	ByProviderModel map[string]endpointModelEntry
	DefaultID       string
	DefaultRef      string
}

type endpointModelEntry struct {
	ID           string
	ModelID      string
	ProviderSpec string
	ProviderRef  string
}

const (
	modelResolutionClient         = "client"
	modelResolutionDefaultMissing = "default_missing"
	modelResolutionDefaultPrimary = "default_primary"
	modelResolutionDefaultUnknown = "default_unknown"
)

func buildEndpointModelCatalog(endpoint endpointintent.Endpoint) endpointModelCatalog {
	configs := endpoint.ProviderConfigs()
	modelCount := map[string]int{}
	providerModelCount := map[string]int{}
	for _, cfg := range configs {
		model := strings.TrimSpace(cfg.ModelID())
		if model == "" {
			continue
		}
		provider := strings.TrimSpace(cfg.ProviderSpec().String())
		modelCount[model]++
		providerModel := CanonicalModelAlias(provider, model)
		providerModelCount[providerModel]++
	}
	entries := make([]endpointModelEntry, 0, len(configs))
	byAlias := make(map[string]endpointModelEntry, len(configs))
	byMechanical := make(map[string]endpointModelEntry, len(configs))
	byProviderModel := make(map[string]endpointModelEntry, len(configs))
	defaultRef := endpoint.SelectedProviderConfigRef().String()
	defaultID := ""
	addEntry := func(id, model, provider, ref string) endpointModelEntry {
		entry := endpointModelEntry{
			ID:           id,
			ModelID:      model,
			ProviderSpec: provider,
			ProviderRef:  ref,
		}
		entries = append(entries, entry)
		return entry
	}
	for _, cfg := range configs {
		model := strings.TrimSpace(cfg.ModelID())
		if model == "" {
			continue
		}
		provider := strings.TrimSpace(cfg.ProviderSpec().String())
		ref := strings.TrimSpace(cfg.Ref().String())
		providerModel := CanonicalModelAlias(provider, model)
		alias := strings.ToLower(strings.TrimSpace(cfg.TargetAlias()))
		mechanical := model
		if modelCount[model] > 1 {
			mechanical = providerModel
		}
		if providerModelCount[providerModel] > 1 {
			mechanical = providerModel + ":" + ref
		}
		actualID := mechanical
		if alias != "" {
			actualID = alias
			aliasEntry := endpointModelEntry{
				ID:           alias,
				ModelID:      model,
				ProviderSpec: provider,
				ProviderRef:  ref,
			}
			byAlias[normalizeSelector(alias)] = aliasEntry
		}
		mechanicalEntry := endpointModelEntry{
			ID:           mechanical,
			ModelID:      model,
			ProviderSpec: provider,
			ProviderRef:  ref,
		}
		byMechanical[normalizeSelector(mechanical)] = mechanicalEntry
		if providerModelCount[providerModel] == 1 {
			byProviderModel[normalizeSelector(providerModel)] = mechanicalEntry
		}
		addEntry(actualID, model, provider, ref)
		if ref == defaultRef {
			defaultID = actualID
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].ID == entries[j].ID {
			return entries[i].ProviderRef < entries[j].ProviderRef
		}
		return entries[i].ID < entries[j].ID
	})
	return endpointModelCatalog{
		Entries:         entries,
		ByAlias:         byAlias,
		ByMechanical:    byMechanical,
		ByProviderModel: byProviderModel,
		DefaultID:       defaultID,
		DefaultRef:      defaultRef,
	}
}

func resolveProviderConfigForRequest(endpoint endpointintent.Endpoint, catalog endpointModelCatalog, requestedModel string) (endpointintent.ProviderConfig, string, error) {
	configs := endpoint.ProviderConfigs()
	selectedRef := endpoint.SelectedProviderConfigRef().String()
	selected := endpoint.SelectedProviderConfig()
	rawSelector := strings.TrimSpace(requestedModel)
	selector := normalizeSelector(rawSelector)
	if selector == "" {
		return selected, modelResolutionDefaultMissing, nil
	}
	if selector == compatibility.PrimaryTargetSelector {
		return selected, modelResolutionDefaultPrimary, nil
	}
	if entry, ok := catalog.ByAlias[selector]; ok {
		for _, cfg := range configs {
			if cfg.Ref().String() == entry.ProviderRef {
				return cfg, modelResolutionClient, nil
			}
		}
	}
	if entry, ok := catalog.ByMechanical[selector]; ok {
		for _, cfg := range configs {
			if cfg.Ref().String() == entry.ProviderRef {
				return cfg, modelResolutionClient, nil
			}
		}
	}
	if entry, ok := catalog.ByProviderModel[selector]; ok {
		for _, cfg := range configs {
			if cfg.Ref().String() == entry.ProviderRef {
				return cfg, modelResolutionClient, nil
			}
		}
	}
	for _, cfg := range configs {
		if cfg.Ref().String() == selectedRef {
			return cfg, modelResolutionDefaultUnknown, nil
		}
	}
	return endpointintent.ProviderConfig{}, "", compatibility.InternalError("selected provider config is not available")
}

func normalizeSelector(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}
