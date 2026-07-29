package canonical

import (
	"fmt"
	"sort"
)

// ToolEffect is one ordered call occurrence and its optional consuming result.
// ResultIndex is -1 while the occurrence remains pending. Executor is specified
// only for discovery effects because execution ownership participates in their
// correlation contract.
type ToolEffect struct {
	CallIndex   int
	ResultIndex int
	CallID      ToolCallID
	Kind        ToolKind
	Executor    Specified[DiscoveryExecutor]
}

// ToolEffectMatcher owns canonical call/result occurrence matching. A call ID
// remains reserved until the matching result consumes it, then a later
// occurrence may reuse that ID without relabelling the earlier effect.
type ToolEffectMatcher struct {
	effects []ToolEffect
	pending map[ToolCallID]int
}

// Accept admits one canonical item at its ordered index. Non-effect items are
// ignored. A non-nil result identifies the effect completed by this item.
func (m *ToolEffectMatcher) Accept(index int, item CanonicalItem) (*ToolEffect, error) {
	if m.pending == nil {
		m.pending = make(map[ToolCallID]int)
	}
	if call, ok := item.ToolCall(); ok {
		return nil, m.acceptCall(index, call)
	}
	if result, ok := item.ToolResult(); ok {
		return m.acceptToolResult(index, result)
	}
	if result, ok := item.ToolDiscoveryResult(); ok {
		return m.acceptDiscoveryResult(index, result)
	}
	return nil, nil
}

func (m *ToolEffectMatcher) acceptCall(index int, call ToolCallItem) error {
	id := call.CallID()
	if _, exists := m.pending[id]; exists {
		return fmt.Errorf("tool call repeats a pending id %q", id.String())
	}
	effect := ToolEffect{
		CallIndex:   index,
		ResultIndex: -1,
		CallID:      id,
		Kind:        call.Tool().Kind(),
	}
	switch effect.Kind {
	case ToolKindFunction, ToolKindCustom, ToolKindWebSearch:
	case ToolKindDiscovery:
		executor, specified := call.DiscoveryExecutor()
		if !specified {
			return fmt.Errorf("tool-discovery call %q has no execution owner", id.String())
		}
		effect.Executor = Specify(executor)
	default:
		return fmt.Errorf("tool call %q has non-callable kind %q", id.String(), effect.Kind)
	}
	m.effects = append(m.effects, effect)
	m.pending[id] = len(m.effects) - 1
	return nil
}

func (m *ToolEffectMatcher) acceptToolResult(index int, result ToolResultItem) (*ToolEffect, error) {
	effect, effectIndex, err := m.pendingEffect(result.CallID())
	if err != nil {
		return nil, err
	}
	_, webSearch := result.WebSearch()
	switch effect.Kind {
	case ToolKindFunction, ToolKindCustom:
		if webSearch {
			return nil, fmt.Errorf("web-search result does not match pending %s call id %q", effect.Kind, effect.CallID.String())
		}
	case ToolKindWebSearch:
		if !webSearch {
			return nil, fmt.Errorf("content result does not match pending web-search call id %q", effect.CallID.String())
		}
	case ToolKindDiscovery:
		return nil, fmt.Errorf("tool result does not match pending discovery call id %q", effect.CallID.String())
	default:
		return nil, fmt.Errorf("tool result has invalid pending call kind %q for id %q", effect.Kind, effect.CallID.String())
	}
	return m.complete(index, effectIndex), nil
}

func (m *ToolEffectMatcher) acceptDiscoveryResult(index int, result ToolDiscoveryResultItem) (*ToolEffect, error) {
	effect, effectIndex, err := m.pendingEffect(result.CallID())
	if err != nil {
		return nil, err
	}
	if effect.Kind != ToolKindDiscovery {
		return nil, fmt.Errorf("tool-discovery result does not match pending %s call id %q", effect.Kind, effect.CallID.String())
	}
	executor, specified := effect.Executor.Get()
	if !specified || executor != result.Executor() {
		return nil, fmt.Errorf("tool-discovery result execution owner differs from call id %q", effect.CallID.String())
	}
	return m.complete(index, effectIndex), nil
}

func (m *ToolEffectMatcher) pendingEffect(id ToolCallID) (ToolEffect, int, error) {
	effectIndex, exists := m.pending[id]
	if !exists {
		return ToolEffect{}, -1, fmt.Errorf("tool result has no pending call id %q", id.String())
	}
	return m.effects[effectIndex], effectIndex, nil
}

func (m *ToolEffectMatcher) complete(resultIndex, effectIndex int) *ToolEffect {
	m.effects[effectIndex].ResultIndex = resultIndex
	effect := m.effects[effectIndex]
	delete(m.pending, effect.CallID)
	return &effect
}

// Pending returns unresolved effects in call order.
func (m *ToolEffectMatcher) Pending() []ToolEffect {
	pending := make([]ToolEffect, 0, len(m.pending))
	for _, effectIndex := range m.pending {
		pending = append(pending, m.effects[effectIndex])
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].CallIndex < pending[j].CallIndex
	})
	return pending
}

// MatchToolEffects applies ToolEffectMatcher to a complete ordered item list.
// The returned effects retain call order and include pending occurrences with a
// ResultIndex of -1; this wrapper contains no second matching algorithm.
func MatchToolEffects(items []CanonicalItem) ([]ToolEffect, error) {
	var matcher ToolEffectMatcher
	for index, item := range items {
		if _, err := matcher.Accept(index, item); err != nil {
			return nil, err
		}
	}
	return append([]ToolEffect(nil), matcher.effects...), nil
}
