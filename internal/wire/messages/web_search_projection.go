package messages

import (
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// projectMessagesWebSearchLifecycles removes only completed call/result pairs
// that Messages cannot express exactly. Open effects are omitted at their call
// occurrence; target spelling gaps never become execution-owned failures.
func projectMessagesWebSearchLifecycles(items []canonical.CanonicalItem, feature canonical.CapabilityPath) ([]canonical.CanonicalItem, []compat.Change, error) {
	drop := map[int]struct{}{}
	changes := make([]compat.Change, 0)
	var matcher canonical.ToolEffectMatcher
	effects := make([]canonical.ToolEffect, 0)
	for index, item := range items {
		if call, ok := item.ToolCall(); ok && call.Tool().Kind() != canonical.ToolKindWebSearch {
			continue
		}
		if _, call := item.ToolCall(); !call {
			result, ok := item.ToolResult()
			if !ok {
				continue
			}
			if _, webSearch := result.WebSearch(); !webSearch {
				continue
			}
		}
		completed, err := matcher.Accept(index, item)
		if err != nil {
			if feature == canonical.ResponseItemsKind {
				return nil, nil, canonical.NewBackendError("messages", 0, "backend returned an invalid web-search lifecycle: "+err.Error(), "")
			}
			return nil, nil, canonical.InternalError("canonical web-search lifecycle is malformed: " + err.Error())
		}
		if completed != nil {
			effects = append(effects, *completed)
		}
	}
	effects = append(effects, matcher.Pending()...)
	for _, effect := range effects {
		if effect.Kind != canonical.ToolKindWebSearch {
			continue
		}
		call, _ := items[effect.CallIndex].ToolCall()
		search, valid := call.Input().WebSearch()
		if valid && search.Action == canonical.WebSearchActionSearch && len(search.Queries) == 1 {
			continue
		}
		drop[effect.CallIndex] = struct{}{}
		if effect.ResultIndex >= 0 {
			drop[effect.ResultIndex] = struct{}{}
		}
		occurrence := canonical.RequestItemOccurrence(uint32(effect.CallIndex))
		if feature == canonical.ResponseItemsKind {
			occurrence = canonical.ResponseItemOccurrence(uint32(effect.CallIndex))
		}
		changes = append(changes, compat.NewOmission(feature, occurrence))
	}
	if len(drop) == 0 {
		return append([]canonical.CanonicalItem(nil), items...), nil, nil
	}
	out := make([]canonical.CanonicalItem, 0, len(items)-len(drop))
	for index, item := range items {
		if _, omitted := drop[index]; omitted {
			continue
		}
		out = append(out, item)
	}
	return out, changes, nil
}
