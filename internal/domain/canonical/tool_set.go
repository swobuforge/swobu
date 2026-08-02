package canonical

import "fmt"

// ToolSet preserves model-visible declaration order while keeping a derived,
// non-authoritative lookup index.
type ToolSet struct {
	ordered []ToolDeclaration
	byKey   map[string]int
}

func NewToolSet(declarations []ToolDeclaration) (ToolSet, error) {
	if declarations == nil {
		return ToolSet{}, nil
	}
	ordered := cloneToolDeclarations(declarations)
	return buildToolSet(ordered)
}

// newToolSetOwned adopts a slice that the caller has already detached from
// canonical state without re-cloning. The single intended caller is
// ToolEnvironmentAt, which builds its ordered slice from boundary accessors
// (ToolSet.Declarations, ToolNamespace.Tools, MCPToolSource.Tools) that already
// return independent clones; re-cloning there is pure waste (epic-50 task 070:
// this path was the #1 allocator on the live daemon profile).
//
// Invariant the caller must hold: ordered contains declarations already
// detached from canonical item state, never aliasing it. buildToolSet performs
// the same validation as NewToolSet (invalid kind, duplicate key) so a stale
// or aliased slice cannot slip through undetected.
func newToolSetOwned(ordered []ToolDeclaration) (ToolSet, error) {
	return buildToolSet(ordered)
}

func buildToolSet(ordered []ToolDeclaration) (ToolSet, error) {
	if ordered == nil {
		return ToolSet{}, nil
	}
	byKey := make(map[string]int, len(ordered))
	for index, declaration := range ordered {
		key := declaration.Key().String()
		if declaration.Kind() == "" || key == "" {
			return ToolSet{}, fmt.Errorf("canonical tool set contains an invalid declaration")
		}
		if _, duplicate := byKey[key]; duplicate {
			return ToolSet{}, fmt.Errorf("canonical tool set contains duplicate key %q", key)
		}
		byKey[key] = index
	}
	return ToolSet{ordered: ordered, byKey: byKey}, nil
}

func (s ToolSet) Declarations() []ToolDeclaration { return cloneToolDeclarations(s.ordered) }

func (s ToolSet) Lookup(key ToolKey) (ToolDeclaration, bool) {
	index, ok := s.byKey[key.String()]
	if ok && index >= 0 && index < len(s.ordered) {
		return s.ordered[index].Clone(), true
	}
	return ToolDeclaration{}, false
}

func (s ToolSet) IsEmpty() bool { return len(s.ordered) == 0 }

func (s ToolSet) Clone() ToolSet {
	cloned, err := NewToolSet(s.ordered)
	if err != nil {
		return ToolSet{}
	}
	return cloned
}
