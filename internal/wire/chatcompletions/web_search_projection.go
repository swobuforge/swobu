package chatcompletions

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// projectChatCompletionsWebSearchLifecycles removes only completed
// provider-owned web-search call/result pairs. Chat Completions has no response
// grammar for that lifecycle or its citation metadata, but its final assistant
// text remains a valid portable client projection.
func projectChatCompletionsWebSearchLifecycles(items []canonical.CanonicalItem) ([]canonical.CanonicalItem, []compat.Decision, error) {
	drop := map[int]struct{}{}
	decisions := make([]compat.Decision, 0)
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
		decisions = append(decisions, compat.Decision{
			Feature: compat.ResponseItemsKind,
			Outcome: compat.Drop,
			Subject: compat.Subject("web_search:" + effect.CallID.String()),
		})
	}

	projected := make([]canonical.CanonicalItem, 0, len(items)-len(drop))
	for index, item := range items {
		if _, omitted := drop[index]; omitted {
			continue
		}
		projected = append(projected, item)
		decisions = append(decisions, chatCompletionsCitationDropDecisions(uint32(index), item)...)
	}
	if len(drop) > 0 && len(projected) == 0 {
		return nil, nil, canonical.NewBackendError(
			"", 0, "backend response has no Chat Completions semantics after web-search projection", "",
		)
	}
	return projected, decisions, nil
}

func chatCompletionsCitationDropDecisions(itemOrdinal uint32, item canonical.CanonicalItem) []compat.Decision {
	message, ok := item.Message()
	if !ok {
		return nil
	}
	decisions := make([]compat.Decision, 0)
	for partOrdinal, part := range message.Content() {
		if _, ok := part.Text(); !ok || len(part.Citations()) == 0 {
			continue
		}
		decisions = append(decisions, compat.Decision{
			Feature: compat.ResponseItemsMessageCitations,
			Outcome: compat.Drop,
			Subject: compat.Subject(fmt.Sprintf("citation:%d:%d", itemOrdinal, partOrdinal)),
		})
	}
	return decisions
}
