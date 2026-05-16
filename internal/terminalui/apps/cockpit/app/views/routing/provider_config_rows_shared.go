package routing

import (
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// canonicalProviderConfigRows holds the core provider-config row set shared by
// create/workspace-edit/add-model flows. Keeping this list centralized avoids
// drift where one flow silently drops a core field (for example frame).
type canonicalProviderConfigRows struct {
	Provider retained.ViewSpec[state.Model]
	Auth     retained.ViewSpec[state.Model]
	Frame    retained.ViewSpec[state.Model]
	Model    retained.ViewSpec[state.Model]
}

func appendCanonicalProviderConfigRows(rows []retained.ViewSpec[state.Model], keyPrefix string, shared canonicalProviderConfigRows) []retained.ViewSpec[state.Model] {
	key := func(field string) string {
		if keyPrefix == "" {
			return field
		}
		return keyPrefix + "/" + field
	}
	if shared.Provider != nil {
		rows = append(rows, retained.Named[state.Model](key("provider"), shared.Provider))
	}
	if shared.Auth != nil {
		rows = append(rows, retained.Named[state.Model](key("credentials"), shared.Auth))
	}
	if shared.Frame != nil {
		rows = append(rows, retained.Named[state.Model](key("frame"), shared.Frame))
	}
	if shared.Model != nil {
		rows = append(rows, retained.Named[state.Model](key("model"), shared.Model))
	}
	return rows
}
