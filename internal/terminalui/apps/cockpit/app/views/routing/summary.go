// Pure formatting functions for routing section.
// No side effects, no state mutation.
package routing

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
)

func createRunOnSummary(model state.Model) string {
	pc := selectors.CreateDraftProviderConfig(model)
	if pc == nil {
		return "not set"
	}
	return providerConfigSummary(*pc)
}

func selectedDefaultModelSummary(model state.Model, snapshot *state.EndpointSnapshot) string {
	pc := selectors.SelectedProviderConfig(model, snapshot)
	if pc == nil {
		return selectors.EmptyOr(snapshot.SelectedProviderConfigRef, "not selected")
	}
	if selector := selectors.ProviderConfigRequestModelID(snapshot, pc.Ref); selector != "" {
		return selector
	}
	return providerConfigSummary(*pc)
}

func modelSummary(model state.Model, entry *state.CatalogEntry) string {
	if entry == nil {
		return "not selected"
	}
	selected := selectors.SelectedModelID(model, entry)
	if len(entry.ModelIDs) <= 1 {
		return selected
	}
	return fmt.Sprintf("%s (+%d)", selected, len(entry.ModelIDs)-1)
}

func providerConfigSummary(pc state.ProviderConfigSnapshot) string {
	alias := strings.TrimSpace(pc.TargetAlias)
	if alias != "" {
		return alias
	}
	specID := strings.TrimSpace(pc.ProviderSpec)
	spec := providercatalog.DisplayName(specID)
	model := strings.TrimSpace(pc.ModelID)
	if model == "" {
		return spec
	}
	return model
}
