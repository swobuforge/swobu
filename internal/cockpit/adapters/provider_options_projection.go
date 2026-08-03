package adapters

import (
	"sort"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/profile"
)

// operatorProviderOptions returns provider picker options from the profile
// catalog, filtered to VisibleInOperatorUI and ordered by providerPickerOrder.
// This is pure projection with no daemon IO; it belongs inside readmodel
// hydration, not a separate query port.
func operatorProviderOptions() []readmodel.ProviderOptionReadModel {
	profiles := profile.All()
	sort.SliceStable(profiles, func(i, j int) bool {
		left, right := providerPickerOrder(profiles[i]), providerPickerOrder(profiles[j])
		if left != right {
			return left < right
		}
		return profiles[i].ProviderDisplayName < profiles[j].ProviderDisplayName
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

func providerPickerOrder(p profile.Profile) int {
	switch p.ProviderID {
	case profile.ProviderSpecOpenAI:
		return 0
	case profile.ProviderSpecChatGPT:
		return 1
	case profile.ProviderSpecAnthropic:
		return 2
	case profile.ProviderSpecDeepSeek:
		return 3
	case profile.ProviderSpecOpenRouter:
		return 4
	case profile.ProviderSpecZAI:
		return 5
	case profile.ProviderSpecOllama:
		return 6
	case profile.ProviderSpecAzure:
		return 7
	case profile.ProviderSpecCustom:
		return 8
	default:
		return 100
	}
}
