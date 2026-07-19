package responses

import (
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// responsesToolWireName returns the Responses wire token for one function or
// custom tool declaration.
//
// Plain tools stay raw on the Responses wire. Only namespace-bearing tool IDs
// need projection, because the flat wire shape has to carry that extra scope.
func responsesToolWireName(tool canonical.ToolDecl) (string, error) {
	switch decl := tool.(type) {
	case canonical.FunctionToolDecl:
		return responsesFunctionToolWireName(decl)
	case *canonical.FunctionToolDecl:
		return responsesFunctionToolWireName(*decl)
	case canonical.CustomToolDecl:
		return responsesCustomToolWireName(decl)
	case *canonical.CustomToolDecl:
		return responsesCustomToolWireName(*decl)
	default:
		return "", canonical.UnsupportedOperation("responses protocol only supports known tool declaration types")
	}
}

func responsesFunctionToolWireName(decl canonical.FunctionToolDecl) (string, error) {
	return responsesProjectedOrRawToolName(decl.ToolID(), decl.ToolName(), decl)
}

func responsesCustomToolWireName(decl canonical.CustomToolDecl) (string, error) {
	return responsesProjectedOrRawToolName(decl.ToolID(), decl.ToolName(), decl)
}

func responsesProjectedOrRawToolName(id canonical.SemanticToolID, name string, projector canonical.ToolDecl) (string, error) {
	trimmedName := strings.TrimSpace(name) // swobu:io-string source=boundary
	if trimmedName == "" {
		return "", canonical.BadRequest("response request tool declarations require a name")
	}
	if strings.Contains(strings.TrimSpace(id.Path), "/") { // swobu:io-string source=domain
		projected, err := canonical.ProjectedToolName(projector)
		if err != nil {
			return "", err
		}
		projected = strings.TrimSpace(projected) // swobu:io-string source=boundary
		if projected == "" {
			return "", canonical.BadRequest("response request tool declarations require a name")
		}
		return projected, nil
	}
	return trimmedName, nil
}

// responsesFlatToolIdentityFromWire resolves one flat Responses tool name
// into a canonical tool identity and leaf name.
//
// Reverse projection is proof-based: when a projected form verifies, recover
// the canonical ID; otherwise keep the token raw.
func responsesFlatToolIdentityFromWire(name string, kind canonical.ToolKind, fieldPath, kindDescriptor string) (canonical.SemanticToolID, string, error) {
	trimmed := strings.TrimSpace(name) // swobu:io-string source=boundary
	if trimmed == "" {
		return canonical.SemanticToolID{}, "", canonical.BadRequest("responses request tool declarations require a name")
	}
	id, leaf, _ := canonical.ToolIdentityFromWire(trimmed, kind)
	return id, leaf, nil
}

// responsesResolveToolChoiceByWireName resolves a specific Responses
// tool_choice against the declared surface.
//
// Raw names match plain tools by visible name. Projected names still round-trip
// namespace-bearing tools through the exact canonical ID, but only when reverse
// projection can prove the token.
func responsesResolveToolChoiceByWireName(tools []canonical.ToolDecl, name, specificType, fieldPath string, sink compat.Sink, exchangeID string) (canonical.ToolDecl, string, error) {
	trimmed := strings.TrimSpace(name) // swobu:io-string source=boundary
	if trimmed == "" {
		return nil, "", canonical.BadRequest("responses request tool_choice specific requires a tool name")
	}
	normalizedType := strings.ToLower(strings.TrimSpace(specificType)) // swobu:io-string source=boundary
	kind := canonical.ToolKind(normalizedType)
	if id, _, ok := canonical.ToolIdentityFromWire(trimmed, kind); ok {
		resolved, resolvedType, resolveErr := canonical.ResolveToolDeclByID(tools, id, normalizedType)
		if resolveErr == nil {
			if err := emitResponsesToolNameNamespaceDecision(sink, exchangeID, nil, compat.Exact, compat.Subject("wire:/tool_choice/name")); err != nil {
				return nil, "", err
			}
			return resolved, resolvedType, nil
		}
	}
	resolved, resolvedType, err := canonical.ResolveToolDeclByName(tools, trimmed, normalizedType)
	if err != nil {
		return nil, "", wrapResponsesToolReferenceError(fieldPath, normalizedType, trimmed, err)
	}
	return resolved, resolvedType, nil
}
