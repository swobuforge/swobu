package tools

import (
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/report"
)

// Result reports tool declaration normalization effects.
type Result struct {
	Mutated bool
	Losses  []report.Loss
}

// NormalizeDeclarations rewrites supported tool declarations to function form
// and removes unsupported forms with explicit projection loss.
func NormalizeDeclarations(doc carrier.WireDocument) (carrier.WireDocument, Result, error) {
	if len(doc.Raw) == 0 {
		return doc, Result{}, nil
	}
	losses := make([]report.Loss, 0)
	out, mutated, err := carrier.MutateWireDocumentJSON(doc, func(payload map[string]any) (bool, error) {
		rawTools, ok := payload["tools"]
		if !ok {
			return false, nil
		}
		tools, ok := rawTools.([]any)
		if !ok {
			return false, nil
		}
		changed := false
		normalized := make([]any, 0, len(tools))
		for _, entry := range tools {
			tool, ok := entry.(map[string]any)
			if !ok {
				changed = true
				losses = append(losses, report.Loss{Field: "tools", Kind: report.LossUnrepresentableTool, Reason: "unsupported_tool_removed", Severity: report.SeverityWarning})
				continue
			}
			typeName, _ := tool["type"].(string)
			switch typeName {
			case "function":
				normalized = append(normalized, tool)
			case "namespace":
				changed = true
				if fn, ok := tool["function"].(map[string]any); ok {
					normalized = append(normalized, map[string]any{"type": "function", "function": fn})
					continue
				}
				losses = append(losses, report.Loss{Field: "tools", Kind: report.LossUnrepresentableTool, Reason: "namespace_tool_without_function_removed", Severity: report.SeverityWarning})
			default:
				changed = true
				losses = append(losses, report.Loss{Field: "tools", Kind: report.LossUnrepresentableTool, Reason: "unsupported_tool_removed", Severity: report.SeverityWarning})
			}
		}
		if !changed {
			return false, nil
		}
		payload["tools"] = normalized
		return true, nil
	})
	if err != nil {
		return carrier.WireDocument{}, Result{}, err
	}
	return out, Result{Mutated: mutated, Losses: losses}, nil
}
