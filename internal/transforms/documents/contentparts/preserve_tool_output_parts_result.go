package contentparts

import (
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/report"
)

// Result reports structured content-part preservation effects.
type Result struct {
	Mutated bool
	Losses  []report.Loss
}

// PreserveToolOutputParts keeps tool output content parts in structured form.
// Unsupported parts are removed with explicit projection loss.
// swobu:codelint ignore string-switch io boundary decode fanout over external part types
func PreserveToolOutputParts(doc carrier.WireDocument, supportsFileParts bool) (carrier.WireDocument, Result, error) {
	if len(doc.Raw) == 0 {
		return doc, Result{}, nil
	}
	losses := make([]report.Loss, 0)
	out, mutated, err := carrier.MutateWireDocumentJSON(doc, func(payload map[string]any) (bool, error) {
		inputRaw, ok := payload["input"]
		if !ok {
			return false, nil
		}
		inputItems, ok := inputRaw.([]any)
		if !ok {
			return false, nil
		}
		changed := false
		for i := range inputItems {
			item, ok := inputItems[i].(map[string]any)
			if !ok {
				continue
			}
			typ, _ := item["type"].(string)
			if typ != "function_call_output" {
				continue
			}
			outputRaw, exists := item["output"]
			if !exists {
				continue
			}
			parts, ok := outputRaw.([]any)
			if !ok {
				continue
			}
			filtered := make([]any, 0, len(parts))
			for _, p := range parts {
				part, ok := p.(map[string]any)
				if !ok {
					changed = true
					losses = append(losses, report.Loss{
						Field:    "input[].output[]",
						Kind:     report.LossUnrepresentableContentPart,
						Reason:   "unsupported_tool_output_part_removed",
						Severity: report.SeverityWarning,
					})
					continue
				}
				partType, _ := part["type"].(string)
				switch partType {
				case "text", "output_text", "input_text":
					filtered = append(filtered, part)
				case "input_file", "output_file", "file":
					if supportsFileParts {
						filtered = append(filtered, part)
						continue
					}
					changed = true
					losses = append(losses, report.Loss{
						Field:    "input[].output[]",
						Kind:     report.LossUnrepresentableContentPart,
						Reason:   "unsupported_file_part_removed",
						Severity: report.SeverityWarning,
					})
				default:
					changed = true
					losses = append(losses, report.Loss{
						Field:    "input[].output[]",
						Kind:     report.LossUnrepresentableContentPart,
						Reason:   "unsupported_tool_output_part_removed",
						Severity: report.SeverityWarning,
					})
				}
			}
			item["output"] = filtered
		}
		return changed, nil
	})
	if err != nil {
		return carrier.WireDocument{}, Result{}, err
	}
	return out, Result{Mutated: mutated, Losses: losses}, nil
}
