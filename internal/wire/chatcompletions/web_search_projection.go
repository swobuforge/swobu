package chatcompletions

import (
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// projectChatCompletionsWebSearchLifecycles removes only completed
// provider-owned web-search call/result pairs. Chat Completions has no response
// grammar for that lifecycle or its citation metadata, but its final assistant
// text remains a valid portable client projection.
func projectChatCompletionsWebSearchLifecycles(items []canonical.CanonicalItem) ([]canonical.CanonicalItem, []compat.Change, error) {
	drop := map[int]struct{}{}
	changes := make([]compat.Change, 0)
	effects, err := canonical.MatchToolEffects(items)
	if err != nil {
		return nil, nil, canonical.NewBackendError("", 0, "backend returned an invalid tool-effect lifecycle: "+err.Error(), "")
	}
	for _, effect := range effects {
		if effect.Kind != canonical.ToolKindWebSearch {
			continue
		}
		if effect.ResultIndex < 0 {
			return nil, nil, canonical.NewBackendError(
				"", 0, "backend returned an unresolved web-search lifecycle to a Chat Completions client", "",
			)
		}
		drop[effect.CallIndex] = struct{}{}
		drop[effect.ResultIndex] = struct{}{}
		changes = append(changes, compat.Change{
			Capability: canonical.ResponseItemsKind,
			Kind:       compat.Omission,
			Occurrence: canonical.CallOccurrence(effect.CallID),
		})
	}

	projected := make([]canonical.CanonicalItem, 0, len(items)-len(drop))
	for index, item := range items {
		if _, omitted := drop[index]; omitted {
			continue
		}
		projected = append(projected, item)
		changes = append(changes, chatCompletionsCitationDropDecisions(uint32(index), item)...)
	}
	if len(drop) > 0 && len(projected) == 0 {
		return nil, nil, canonical.NewBackendError(
			"", 0, "backend response has no Chat Completions semantics after web-search projection", "",
		)
	}
	return projected, changes, nil
}

func chatCompletionsCitationDropDecisions(itemOrdinal uint32, item canonical.CanonicalItem) []compat.Change {
	message, ok := item.Message()
	if !ok {
		return nil
	}
	changes := make([]compat.Change, 0)
	for partOrdinal, part := range message.Content() {
		if _, ok := part.Text(); !ok || len(part.Citations()) == 0 {
			continue
		}
		changes = append(changes, compat.Change{
			Capability: canonical.ResponseItemsMessageCitations,
			Kind:       compat.Omission,
			Occurrence: canonical.ResponsePartOccurrence(canonical.ItemPosition{Item: itemOrdinal, Part: uint32(partOrdinal)}),
		})
	}
	return changes
}
