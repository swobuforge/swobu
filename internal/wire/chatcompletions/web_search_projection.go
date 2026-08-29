package chatcompletions

import (
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// projectChatCompletionsRequestHistory composes target-local history losses:
// provider-owned effect projection followed by citation accounting on every
// surviving message part. It does not mutate canonical history.
func projectChatCompletionsRequestHistory(items []canonical.CanonicalItem) ([]canonical.CanonicalItem, []compat.Change, error) {
	projected, changes, err := projectChatCompletionsRequestWebSearchLifecycles(items)
	if err != nil {
		return nil, nil, err
	}
	for index, item := range items {
		changes = append(changes, chatCompletionsRequestCitationDropDecisions(uint32(index), item)...)
	}
	return projected, changes, nil
}

// projectChatCompletionsRequestWebSearchLifecycles removes provider-owned
// search and discovery effects as occurrence-atomic call/result pairs. The
// canonical matcher owns correlation, including call-ID reuse. Open effects
// are omitted explicitly because Chat has no provider-owned lifecycle carrier.
func projectChatCompletionsRequestWebSearchLifecycles(items []canonical.CanonicalItem) ([]canonical.CanonicalItem, []compat.Change, error) {
	drop := map[int]struct{}{}
	changes := make([]compat.Change, 0)
	var matcher canonical.ToolEffectMatcher
	effects := make([]canonical.ToolEffect, 0)
	for index, item := range items {
		if !chatCompletionsOmitsProviderEffect(item) {
			continue
		}
		completed, err := matcher.Accept(index, item)
		if err != nil {
			return nil, nil, canonical.InternalError("canonical web-search history cannot be correlated")
		}
		if completed != nil {
			effects = append(effects, *completed)
		}
	}
	effects = append(effects, matcher.Pending()...)
	for _, effect := range effects {
		if effect.Kind != canonical.ToolKindWebSearch && effect.Kind != canonical.ToolKindDiscovery {
			continue
		}
		drop[effect.CallIndex] = struct{}{}
		if effect.ResultIndex >= 0 {
			drop[effect.ResultIndex] = struct{}{}
		}
		changes = append(changes, compat.NewOmission(canonical.RequestItemsKind, canonical.RequestItemOccurrence(uint32(effect.CallIndex))))
	}
	projected := make([]canonical.CanonicalItem, 0, len(items)-len(drop))
	for index, item := range items {
		if _, omitted := drop[index]; omitted {
			continue
		}
		projected = append(projected, item)
	}
	return projected, changes, nil
}

func chatCompletionsOmitsProviderEffect(item canonical.CanonicalItem) bool {
	if call, ok := item.ToolCall(); ok {
		if call.Tool().Kind() == canonical.ToolKindWebSearch {
			return true
		}
		executor, specified := call.DiscoveryExecutor()
		return specified && executor == canonical.DiscoveryExecutorProvider
	}
	if result, ok := item.ToolResult(); ok {
		_, webSearch := result.WebSearch()
		return webSearch
	}
	if result, ok := item.ToolDiscoveryResult(); ok {
		return result.Executor() == canonical.DiscoveryExecutorProvider
	}
	return false
}

func chatCompletionsRequestCitationDropDecisions(itemOrdinal uint32, item canonical.CanonicalItem) []compat.Change {
	message, ok := item.Message()
	if !ok {
		return nil
	}
	changes := make([]compat.Change, 0)
	for partOrdinal, part := range message.Content() {
		if _, ok := part.Text(); !ok || len(part.Citations()) == 0 {
			continue
		}
		changes = append(changes, compat.NewOmission(
			canonical.RequestItemsMessageCitations,
			canonical.RequestPartOccurrence(canonical.RequestPartRef{Item: itemOrdinal, Part: uint32(partOrdinal)}),
		))
	}
	return changes
}

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
		changes = append(changes, compat.NewOmission(
			canonical.ResponseItemsKind, canonical.ResponseItemOccurrence(uint32(effect.CallIndex)),
		))
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
