package responses

import (
	"strings"

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
func responsesFlatToolIdentityFromWire(name string, kind canonical.ToolKind, fieldPath, kindDescriptor string) (canonical.SemanticToolID, string, error) {
	trimmed := strings.TrimSpace(name) // swobu:io-string source=boundary
	if trimmed == "" {
		return canonical.SemanticToolID{}, "", canonical.BadRequest("responses request tool declarations require a name")
	}
	if strings.Contains(trimmed, "__") {
		id, leaf, err := canonical.ParseProjectedToolName(trimmed, kind)
		if err != nil {
			return canonical.SemanticToolID{}, "", wrapResponsesToolReferenceError(fieldPath, kindDescriptor, trimmed, err)
		}
		return id, leaf, nil
	}
	return canonical.NewSemanticToolIDFor(canonical.ToolOriginRequest, kind, trimmed), trimmed, nil
}

// responsesResolveToolChoiceByWireName resolves a specific Responses
// tool_choice against the declared surface.
//
// Raw names match plain tools by visible name. Projected names still round-trip
// namespace-bearing tools through the exact canonical ID.
func responsesResolveToolChoiceByWireName(tools []canonical.ToolDecl, name, specificType, fieldPath string) (canonical.ToolDecl, string, error) {
	trimmed := strings.TrimSpace(name) // swobu:io-string source=boundary
	if trimmed == "" {
		return nil, "", canonical.BadRequest("responses request tool_choice specific requires a tool name")
	}
	normalizedType := strings.ToLower(strings.TrimSpace(specificType)) // swobu:io-string source=boundary
	if strings.Contains(trimmed, "__") {
		kind := canonical.ToolKind(normalizedType)
		id, _, err := canonical.ParseProjectedToolName(trimmed, kind)
		if err != nil {
			return nil, "", wrapResponsesToolReferenceError(fieldPath, normalizedType, trimmed, err)
		}
		resolved, resolvedType, err := canonical.ResolveToolDeclByID(tools, id, normalizedType)
		if err != nil {
			return nil, "", wrapResponsesToolReferenceError(fieldPath, normalizedType, trimmed, err)
		}
		return resolved, resolvedType, nil
	}
	resolved, resolvedType, err := responsesResolvePlainToolByName(tools, trimmed, normalizedType)
	if err != nil {
		return nil, "", wrapResponsesToolReferenceError(fieldPath, normalizedType, trimmed, err)
	}
	return resolved, resolvedType, nil
}

func responsesResolvePlainToolByName(tools []canonical.ToolDecl, name, specificType string) (canonical.ToolDecl, string, error) {
	normalizedSpecific := strings.ToLower(strings.TrimSpace(specificType)) // swobu:io-string source=boundary
	var (
		found     canonical.ToolDecl
		foundType string
		matched   bool
	)
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		toolType := canonical.ToolDeclKind(tool)
		if normalizedSpecific != "" && toolType != normalizedSpecific {
			continue
		}
		if strings.Contains(strings.TrimSpace(tool.ToolID().Path), "/") { // swobu:io-string source=domain
			continue
		}
		if strings.TrimSpace(tool.ToolName()) != name { // swobu:io-string source=domain
			continue
		}
		if matched && (found.ToolID() != tool.ToolID() || foundType != toolType) {
			return nil, "", canonical.BadRequest("canonical request tool references are ambiguous")
		}
		found = tool
		foundType = toolType
		matched = true
	}
	if !matched {
		return nil, "", canonical.BadRequest("canonical request tool references are undeclared tool")
	}
	return found, foundType, nil
}
