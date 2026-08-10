package adapters

import (
	"sort"
	"strings"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/profile"
)

// operatorProviderOptions returns visible provider picker options from the
// profile catalog in case-insensitive display-name order.
// This is pure projection with no daemon IO; it belongs inside readmodel
// hydration, not a separate query port.
func operatorProviderOptions() []readmodel.ProviderOptionReadModel {
	profiles := profile.All()
	sort.SliceStable(profiles, func(i, j int) bool {
		return strings.ToLower(profiles[i].ProviderDisplayName) <
			strings.ToLower(profiles[j].ProviderDisplayName)
	})

	opts := make([]readmodel.ProviderOptionReadModel, 0, len(profiles))
	for _, p := range profiles {
		if !p.VisibleInOperatorUI {
			continue
		}
		opts = append(opts, readmodel.ProviderOptionReadModel{
			ProviderSpec: string(p.ProviderID),
			DisplayName:  p.ProviderDisplayName,
			SetupHint:    p.SetupHint,
		})
	}
	return opts
}
