package chatgpt

import (
	_ "embed"
	"encoding/json"
	"slices"
	"strings"
	"sync"
)

//go:embed data/chatgpt_subscriptions.json
var chatGPTSubscriptionsRaw []byte

type chatGPTSubscriptionsSnapshot struct {
	Tiers map[string][]string `json:"tiers"`
}

var (
	loadTierCatalogOnce sync.Once
	loadTierCatalogErr  error
	tierCatalog         map[string][]string
)

func chatGPTTierModelIDs(tier string) ([]string, bool) {
	loadChatGPTTierCatalog()
	if loadTierCatalogErr != nil {
		return nil, false
	}
	models, ok := tierCatalog[strings.ToLower(strings.TrimSpace(tier))] // swobu:io-string source=boundary
	if !ok {
		return nil, false
	}
	return slices.Clone(models), true
}

func loadChatGPTTierCatalog() {
	loadTierCatalogOnce.Do(func() {
		var chat chatGPTSubscriptionsSnapshot
		if err := json.Unmarshal(chatGPTSubscriptionsRaw, &chat); err != nil {
			loadTierCatalogErr = err
			return
		}
		out := make(map[string][]string, len(chat.Tiers))
		for tier, items := range chat.Tiers {
			normalizedTier := strings.ToLower(strings.TrimSpace(tier)) // swobu:io-string source=boundary
			if normalizedTier == "" {
				continue
			}
			set := make(map[string]struct{}, len(items))
			for _, item := range items {
				modelID := strings.TrimSpace(item) // swobu:io-string source=boundary
				if modelID == "" {
					continue
				}
				set[modelID] = struct{}{}
			}
			models := make([]string, 0, len(set))
			for modelID := range set {
				models = append(models, modelID)
			}
			slices.Sort(models)
			out[normalizedTier] = models
		}
		tierCatalog = out
	})
}
