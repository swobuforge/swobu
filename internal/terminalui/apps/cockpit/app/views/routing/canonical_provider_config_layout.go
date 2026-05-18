package routing

import (
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// canonicalProviderConfigLayout holds the core provider-config row set shared by
// create/workspace-edit/add-model flows. Keeping this list centralized avoids
// drift where one flow silently drops a core field (for example frame).
type canonicalProviderConfigLayout struct {
	Provider   retained.ViewSpec[state.Model]
	Credential retained.ViewSpec[state.Model]
	Scope      retained.ViewSpec[state.Model]
	Model      retained.ViewSpec[state.Model]
	Delivery   retained.ViewSpec[state.Model]
}

func appendCanonicalProviderConfigLayout(rows []retained.ViewSpec[state.Model], keyPrefix string, shared canonicalProviderConfigLayout) []retained.ViewSpec[state.Model] {
	key := func(field string) string {
		if keyPrefix == "" {
			return field
		}
		return keyPrefix + "/" + field
	}
	if shared.Provider != nil {
		rows = append(rows, retained.Named[state.Model](key("provider"), shared.Provider))
	}
	if shared.Credential != nil {
		rows = append(rows, retained.Named[state.Model](key("credential"), shared.Credential))
	}
	if shared.Scope != nil {
		rows = append(rows, retained.Named[state.Model](key("scope"), shared.Scope))
	}
	if shared.Model != nil {
		rows = append(rows, retained.Named[state.Model](key("model"), shared.Model))
	}
	if shared.Delivery != nil {
		rows = append(rows, retained.Named[state.Model](key("delivery"), shared.Delivery))
	}
	return rows
}
