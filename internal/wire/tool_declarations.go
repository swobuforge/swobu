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

// HasDeferredTools reports whether any declaration contribution or loaded
// discovery result carries deferred visibility.
func HasDeferredTools(items []canonical.CanonicalItem) bool {
	for _, item := range items {
		if declarations, ok := item.ToolDeclarations(); ok && len(declarations.Visibility().DeferredKeys()) > 0 {
			return true
		}
		if result, ok := item.ToolDiscoveryResult(); ok && len(result.Visibility().DeferredKeys()) > 0 {
			return true
		}
	}
	return false
}

// PrepareFlatToolSet removes namespace containers while retaining
// their callable descendants in declaration order. The target supplies the
// exact wire identity used by every later declaration, choice, call, and result
// reference. This check runs after attempt-local name allocation so equal leaf
// names in different namespaces remain distinct; true wire collisions still
// reject the candidate instead of inventing inverse alias state.
func PrepareFlatToolSet(
	declarations []canonical.ToolDeclaration,
	targetIdentity func(canonical.ToolDeclaration) (string, error),
) (FlatToolSet, error) {
	var flattened []canonical.ToolDeclaration
	flattenedNamespaces := 0
	var appendDeclaration func(canonical.ToolDeclaration) error
	appendDeclaration = func(declaration canonical.ToolDeclaration) error {
		if declaration.Kind() == canonical.ToolKindMCP {
			// Exchange owns MCP source resolution and execution. Reaching a
			// provider codec with a residual source would otherwise create a
			// second execution owner or silently erase an available tool surface.
			return provider.IncompatibleCapability(canonical.RequestToolsKind, canonical.ToolOccurrence(declaration.Key()), "provider lowering received an MCP source before Exchange projection")
		}
		namespace, ok := declaration.Namespace()
		if !ok {
			flattened = append(flattened, declaration.Clone())
			return nil
		}
		flattenedNamespaces++
		for _, child := range namespace.Tools() {
			if err := appendDeclaration(child); err != nil {
				return err
			}
		}
		return nil
	}
	for _, declaration := range declarations {
		if err := appendDeclaration(declaration); err != nil {
			return FlatToolSet{}, err
		}
	}
	seen := make(map[string]struct{}, len(flattened))
	for _, declaration := range flattened {
		wireIdentity, err := targetIdentity(declaration)
		if err != nil {
			return FlatToolSet{}, err
		}
		wireIdentity = strings.TrimSpace(wireIdentity)
		if _, duplicate := seen[wireIdentity]; duplicate {
			return FlatToolSet{}, provider.IncompatibleCapability(canonical.RequestToolsName, canonical.Occurrence{}, "flat provider tool declarations require unique target identities")
		}
		seen[wireIdentity] = struct{}{}
	}
	return FlatToolSet{Declarations: flattened, RemovedNamespaces: flattenedNamespaces}, nil
}
