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
	if !ok || index < 0 || index >= len(s.ordered) {
		return ToolDeclaration{}, false
	}
	return s.ordered[index].Clone(), true
}

func (s ToolSet) IsEmpty() bool { return len(s.ordered) == 0 }

func (s ToolSet) Clone() ToolSet {
	cloned, err := NewToolSet(s.ordered)
	if err != nil {
		return ToolSet{}
	}
	return cloned
}
