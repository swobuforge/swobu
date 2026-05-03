package requestpath

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

// BackendModelCapabilityCatalog resolves execution-time capability truth from
// backend model entity identity. Unknown entities fail closed.
type BackendModelCapabilityCatalog struct {
	byExactModel map[backendModelCapabilityKey]CapabilitySnapshot
	byAnyModel   map[backendModelCapabilityScope]CapabilitySnapshot
}

type backendModelCapabilityRecord struct {
	ProviderSpec   string
	ProtocolKind   protocolsurface.Kind
	BackendModelID string
	Capability     CapabilitySnapshot
}

type backendModelCapabilityScope struct {
	ProviderSpec string
	ProtocolKind protocolsurface.Kind
}

type backendModelCapabilityKey struct {
	backendModelCapabilityScope
	BackendModelID string
}

func newBackendModelCapabilityCatalog(records []backendModelCapabilityRecord) BackendModelCapabilityCatalog {
	byExact := make(map[backendModelCapabilityKey]CapabilitySnapshot, len(records))
	byAny := make(map[backendModelCapabilityScope]CapabilitySnapshot, len(records))
	for _, record := range records {
		scope, ok := normalizeCapabilityScope(record.ProviderSpec, record.ProtocolKind)
		if !ok {
			continue
		}
		model := strings.TrimSpace(record.BackendModelID)
		if model == "" || model == "*" {
			byAny[scope] = record.Capability
			continue
		}
		byExact[backendModelCapabilityKey{
			backendModelCapabilityScope: scope,
			BackendModelID:              model,
		}] = record.Capability
	}
	return BackendModelCapabilityCatalog{
		byExactModel: byExact,
		byAnyModel:   byAny,
	}
}

func (catalog BackendModelCapabilityCatalog) SnapshotFor(entity BackendModelEntity) CapabilitySnapshot {
	scope, ok := normalizeCapabilityScope(entity.ProviderSpec, entity.ProtocolKind)
	if !ok {
		return CapabilitySnapshot{}
	}
	modelID := strings.TrimSpace(entity.BackendModelID)
	if modelID == "" {
		return CapabilitySnapshot{}
	}
	if capability, ok := catalog.byExactModel[backendModelCapabilityKey{
		backendModelCapabilityScope: scope,
		BackendModelID:              modelID,
	}]; ok {
		return capability
	}
	if capability, ok := catalog.byAnyModel[scope]; ok {
		return capability
	}
	return CapabilitySnapshot{}
}

func defaultBackendModelCapabilityCatalog() BackendModelCapabilityCatalog {
	facts := providercatalog.ToolChoiceCapabilityFacts()
	records := make([]backendModelCapabilityRecord, 0, len(facts))
	for _, fact := range facts {
		records = append(records, backendModelCapabilityRecord{
			ProviderSpec:   fact.ProviderSpec,
			ProtocolKind:   fact.ProtocolKind,
			BackendModelID: fact.ModelID,
			Capability: CapabilitySnapshot{
				ToolChoice: ToolChoiceCapability{
					ImmediateDowngradeRetry: fact.ImmediateDowngradeRetry,
				},
			},
		})
	}
	return newBackendModelCapabilityCatalog(records)
}

func normalizeCapabilityScope(providerSpec string, protocolKind protocolsurface.Kind) (backendModelCapabilityScope, bool) {
	normalizedSpec := strings.TrimSpace(strings.ToLower(providerSpec))
	if normalizedSpec == "" || protocolKind == "" {
		return backendModelCapabilityScope{}, false
	}
	return backendModelCapabilityScope{
		ProviderSpec: normalizedSpec,
		ProtocolKind: protocolKind,
	}, true
}
