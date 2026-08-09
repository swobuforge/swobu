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
	OmittedMCP        int
}

// ValidateFlatToolPolicy proves that declaration erasure left the caller's
// selection constraint executable. A dependency on an erased declaration is
// target incompatibility, not malformed client intent.
func ValidateFlatToolPolicy(policy canonical.ToolPolicy, declarations []canonical.ToolDeclaration) error {
	if err := policy.ValidateForTools(declarations); err != nil {
		return provider.IncompatibleCapability(canonical.RequestToolPolicy, canonical.Occurrence{}, "flat tool projection cannot preserve the canonical tool-selection constraint")
	}
	return nil
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
	omittedMCP := 0
	var appendDeclaration func(canonical.ToolDeclaration) error
	appendDeclaration = func(declaration canonical.ToolDeclaration) error {
		if declaration.Kind() == canonical.ToolKindMCP {
			// Residual provider-native MCP is not user-healable on flat targets.
			// Drop only its declaration; the caller validates selection policy
			// against the surviving surface before dispatch.
			omittedMCP++
			return nil
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
	return FlatToolSet{Declarations: flattened, RemovedNamespaces: flattenedNamespaces, OmittedMCP: omittedMCP}, nil
}
