package responses

import (
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// responsesToolWireName returns the Responses wire token for one function or
// custom tool declaration.
//
// Attempt preparation has already projected every provider-facing key.
func responsesToolWireName(tool canonical.ToolDeclaration) (string, error) {
	if tool.Kind() != canonical.ToolKindFunction && tool.Kind() != canonical.ToolKindCustom {
		return "", canonical.UnsupportedOperation("responses protocol only supports known tool declaration types")
	}
	trimmedName := strings.TrimSpace(tool.Key().Name()) // swobu:io-string source=boundary
	if trimmedName == "" {
		return "", canonical.BadRequest("response request tool declarations require a name")
	}
	return trimmedName, nil
}

// responsesFlatToolIdentityFromWire resolves one flat Responses tool name
// into a canonical tool identity and leaf name.
//
// Reverse projection is proof-based: when a projected form verifies, recover
// the canonical ID; otherwise keep the token raw.
func responsesFlatToolIdentityFromWire(name string, kind canonical.ToolKind, fieldPath, kindDescriptor string) (canonical.ToolKey, string, error) {
	trimmed := strings.TrimSpace(name) // swobu:io-string source=boundary
	if trimmed == "" {
		return canonical.ToolKey{}, "", canonical.BadRequest("responses request tool declarations require a name")
	}
	id, err := canonical.ToolIdentityFromWire(trimmed, kind)
	return id, trimmed, err
}

// responsesResolveToolChoiceByWireName resolves a specific Responses
// tool_choice against the declared surface.
//
// Raw names match plain tools by visible name. Projected names still round-trip
// namespace-bearing tools through the exact canonical ID, but only when reverse
// projection can prove the token.
func responsesResolveToolChoiceByWireName(tools []canonical.ToolDeclaration, name, specificType, fieldPath string, sink compat.Sink, exchangeID string) (canonical.ToolDeclaration, string, error) {
	trimmed := strings.TrimSpace(name) // swobu:io-string source=boundary
	if trimmed == "" {
		return canonical.ToolDeclaration{}, "", canonical.BadRequest("responses request tool_choice specific requires a tool name")
	}
	normalizedType := strings.ToLower(strings.TrimSpace(specificType)) // swobu:io-string source=boundary
	resolved, resolvedType, err := canonical.ResolveToolDeclarationByName(tools, trimmed, normalizedType)
	if err != nil {
		return canonical.ToolDeclaration{}, "", wrapResponsesToolReferenceError(fieldPath, normalizedType, trimmed, err)
	}
	return resolved, resolvedType, nil
}
