package exchange

import (
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// projectOpaqueReplayForTarget projects an attempt-local canonical request
// against the selected exact target generation.
// Opaque reasoning replay is preserved only when its provenance matches the
// target generation. Ineligible opaque replay is omitted; if readable reasoning
// parts exist, they are preserved. If the reasoning item was opaque-only, it
// is removed entirely.
func projectOpaqueReplayForTarget(request canonical.CanonicalRequest, targetID string, targetVersion uint64) (canonical.CanonicalRequest, bool, []compat.Change, error) {
	items := request.Items()
	projectedItems := make([]canonical.CanonicalItem, 0, len(items))
	var changes []compat.Change
	changed := false

	for ordinal, item := range items {
		if item.Kind() != canonical.ItemKindReasoning {
			projectedItems = append(projectedItems, item)
			continue
		}
		reasoning, _ := item.Reasoning()
		opaque := reasoning.Opaque()
		if opaque.IsZero() {
			projectedItems = append(projectedItems, item)
			continue
		}
		if opaque.MatchesTarget(targetID, targetVersion) {
			projectedItems = append(projectedItems, item)
			continue
		}

		// Ineligible opaque replay (unbound or mismatched target).
		changed = true
		changes = append(changes, compat.NewOmission(
			canonical.RequestItemsReasoningReplay,
			canonical.RequestItemOccurrence(uint32(ordinal)),
		))

		if len(reasoning.Parts()) > 0 {
			strippedItem, err := canonical.NewReasoningItem(reasoning.Parts(), canonical.OpaqueThinking{})
			if err != nil {
				return canonical.CanonicalRequest{}, false, nil, err
			}
			projectedItems = append(projectedItems, strippedItem)
		}
		// If reasoning had no readable parts (opaque-only), it is omitted completely.
	}

	if !changed {
		return request, false, nil, nil
	}
	return request.WithItems(projectedItems), true, changes, nil
}
