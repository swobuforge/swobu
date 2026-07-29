package wire

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

// FlatToolSet carries the flat declaration surface and the number of
// namespace containers removed. The count is compatibility evidence for
// hierarchy loss; it is not enough to reconstruct canonical addressing.
type FlatToolSet struct {
	Declarations      []canonical.ToolDeclaration
	RemovedNamespaces int
}

// PrepareFlatToolSet removes namespace containers while retaining
// their callable descendants in declaration order. The target supplies the
// identity used by every later declaration, choice, call, and result reference;
// collisions reject the candidate instead of inventing inverse alias state.
func PrepareFlatToolSet(
	declarations []canonical.ToolDeclaration,
	targetName func(canonical.ToolDeclaration) string,
) (FlatToolSet, error) {
	var flattened []canonical.ToolDeclaration
	flattenedNamespaces := 0
	var appendDeclaration func(canonical.ToolDeclaration)
	appendDeclaration = func(declaration canonical.ToolDeclaration) {
		namespace, ok := declaration.Namespace()
		if !ok {
			flattened = append(flattened, declaration.Clone())
			return
		}
		flattenedNamespaces++
		for _, child := range namespace.Tools() {
			appendDeclaration(child)
		}
	}
	for _, declaration := range declarations {
		appendDeclaration(declaration)
	}
	seen := make(map[string]struct{}, len(flattened))
	for _, declaration := range flattened {
		wireIdentity := strings.TrimSpace(targetName(declaration))
		if _, duplicate := seen[wireIdentity]; duplicate {
			return FlatToolSet{}, provider.NewIncompatibleTarget("flat provider tool declarations require unique target identities")
		}
		seen[wireIdentity] = struct{}{}
	}
	return FlatToolSet{Declarations: flattened, RemovedNamespaces: flattenedNamespaces}, nil
}
