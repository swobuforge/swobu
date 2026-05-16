// Pure formatting functions for routing section.
// No side effects, no state mutation.
package routing

import (
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
	return providerConfigSummary(*pc)
}

func providerConfigSummary(pc state.ProviderConfigSnapshot) string {
	return providerHumanIdentifier(pc)
}
