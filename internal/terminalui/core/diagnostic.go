package core

import "fmt"

// Severity identifies diagnostic seriousness.
type Severity uint8

const (
	DiagnosticWarning Severity = iota
	DiagnosticError
)

// Diagnostic captures one semantic validation result.
type Diagnostic struct {
	Severity Severity
	Path     string
	Message  string
}

// Validate performs semantic integrity checks over keys, stateful nodes, and
// dynamic collections.
func Validate(root Node) []Diagnostic {
	var out []Diagnostic
	validateNode("root", root, &out, false)
	return out
}

func validateNode(path string, node Node, out *[]Diagnostic, requireInteractiveKeys bool) {
	if node.stateful && node.key.Empty() {
		*out = append(*out, Diagnostic{
			Severity: DiagnosticError,
			Path:     path,
			Message:  "stateful node requires key",
		})
	}
	if requireInteractiveKeys && node.key.Empty() && isInteractive(node) && !node.stateful {
		*out = append(*out, Diagnostic{
			Severity: DiagnosticError,
			Path:     path,
			Message:  "interactive child without key inside dynamic collection",
		})
	}

	seen := make(map[Key]struct{}, len(node.children))
	for i, child := range node.children {
		childPath := fmt.Sprintf("%s/%d", path, i)
		if !child.key.Empty() {
			if _, ok := seen[child.key]; ok {
				*out = append(*out, Diagnostic{
					Severity: DiagnosticError,
					Path:     childPath,
					Message:  fmt.Sprintf("duplicate sibling key %q", child.key),
				})
			} else {
				seen[child.key] = struct{}{}
			}
		}
		validateNode(childPath, child, out, requireInteractiveKeys || node.stateful || node.kind == KindList || node.kind == KindTable)
	}
}

func isInteractive(node Node) bool {
	if node.kind == KindAction || node.kind == KindInput {
		return true
	}
	if node.interaction.Focus.Mode != FocusNone {
		return true
	}
	if len(node.interaction.Keymap) > 0 || len(node.interaction.Signals) > 0 {
		return true
	}
	if len(node.interaction.FocusSignals) > 0 {
		return true
	}
	if node.contract.Focus.FocusableWhenEnabled {
		return true
	}
	return false
}
