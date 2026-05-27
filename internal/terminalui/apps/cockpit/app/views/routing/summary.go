// Pure formatting functions for routing section.
// No side effects, no state mutation.
package routing

import (
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
)

func createRunOnSummary(model state.Model) string {
	pc := selectors.CreateDraftProviderConfig(model)
	if pc == nil {
		return views.ValueRequired
	}
	return providerHumanIdentifier(*pc)
}

func selectedDefaultModelSummary(model state.Model, snapshot *state.EndpointSnapshot) string {
	pc := selectors.SelectedProviderConfig(model, snapshot)
	if pc == nil {
		return selectors.EmptyOr(snapshot.SelectedProviderConfigRef, views.ValueRequired)
	}
	return providerHumanIdentifier(*pc)
}
