package messages

import (
	"context"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

// projectMessagesWebSearchLifecycles removes only completed call/result pairs
// that Messages cannot express exactly. The leaf encoders remain strict so an
// unresolved call still rejects and can drive ordinary target fallback.
func projectMessagesWebSearchLifecycles(items []canonical.CanonicalItem, feature compat.Feature) ([]canonical.CanonicalItem, []compat.Decision, error) {
	type callRecord struct {
		index         int
		representable bool
	}
	calls := map[string]callRecord{}
	results := map[string]int{}
	for index, item := range items {
		if call, ok := item.ToolCall(); ok && call.Tool().Kind() == canonical.ToolKindWebSearch {
			search, valid := call.Input().WebSearch()
			calls[call.CallID().String()] = callRecord{
				index:         index,
				representable: valid && search.Action == canonical.WebSearchActionSearch && len(search.Queries) == 1,
			}
			continue
		}
		if result, ok := item.ToolResult(); ok {
			if _, search := result.WebSearch(); search {
				results[result.CallID().String()] = index
			}
		}
	}
	drop := map[int]struct{}{}
	decisions := make([]compat.Decision, 0)
	for callID, call := range calls {
		if call.representable {
			continue
		}
		resultIndex, completed := results[callID]
		if !completed {
			return nil, nil, provider.NewIncompatibleTarget("Messages cannot represent unresolved canonical web-search call " + callID)
		}
		drop[call.index] = struct{}{}
		drop[resultIndex] = struct{}{}
		decisions = append(decisions, compat.Decision{
			Feature: feature,
			Outcome: compat.Drop,
			Subject: compat.Subject("web_search:" + callID),
		})
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
	return out, decisions, nil
}

func commitMessagesProjectionDecisions(sink compat.Sink, exchangeID string, decisions []compat.Decision) error {
	if sink == nil || len(decisions) == 0 {
		return nil
	}
	if err := sink.Commit(context.Background(), exchangeID, decisions); err != nil {
		return canonical.InternalError("compatibility decision sink commit failed")
	}
	return nil
}
