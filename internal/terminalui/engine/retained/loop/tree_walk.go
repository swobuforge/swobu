package loop

import "github.com/metrofun/swobu/internal/terminalui/engine/retained/rendergraph/layout"

// stableKids returns one copy of children ordered by ascending Z while keeping
// same-Z siblings in their original order. Runtime reuses this deterministic
// order for hit-testing, focus traversal walks, and paint traversal.
func stableKids(children []*layout.LayoutNode) []*layout.LayoutNode {
	out := append([]*layout.LayoutNode(nil), children...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].Z > out[j].Z; j-- {
			if out[j-1].Z == out[j].Z {
				break
			}
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
