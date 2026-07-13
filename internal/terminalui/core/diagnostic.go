package core

import "fmt"

// Severity identifies diagnostic seriousness.
type Severity uint8

const (
	// DiagnosticWarning indicates a behavioral concern that does not halt
	// compilation. Used for migration-phase diagnostics that will become
	// errors once callers are fixed.
	DiagnosticWarning Severity = iota
	// DiagnosticError indicates a structural contract violation that
	// prevents safe compilation.
	DiagnosticError
)

func (s Severity) String() string {
	switch s {
	case DiagnosticWarning:
		return "warning"
	case DiagnosticError:
		return "error"
	default:
		return fmt.Sprintf("severity(%d)", s)
	}
}

// Diagnostic captures one semantic validation result.
type Diagnostic struct {
	Severity Severity
	Path     string
	Message  string
}

// Diagnostics aggregates validation results.
type Diagnostics []Diagnostic

// HasErrors reports whether any diagnostic is an error.
func (ds Diagnostics) HasErrors() bool {
	for _, d := range ds {
		if d.Severity == DiagnosticError {
			return true
		}
	}
	return false
}

// Errors returns only the error-level diagnostics.
func (ds Diagnostics) Errors() Diagnostics {
	var out Diagnostics
	for _, d := range ds {
		if d.Severity == DiagnosticError {
			out = append(out, d)
		}
	}
	return out
}

// Validate performs semantic integrity checks over keys, stateful nodes,
// focusable nodes, actions, and dynamic collections.
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

	// A focusable node should accept at least one intent.
	// KindInput is exempt: it receives built-in edit intents from the lowerer.
	if node.interaction.Focus.Mode == Focusable &&
		node.kind != KindInput &&
		len(node.interaction.Keymap) == 0 {
		*out = append(*out, Diagnostic{
			Severity: DiagnosticWarning,
			Path:     path,
			Message:  "focusable node without accepted intent",
		})
	}

	// An action node should emit at least one signal when activated
	// or on focus. Actions that only emit focus signals are currently
	// valid (migration-phase leniency).
	if node.kind == KindAction && len(node.interaction.Signals) == 0 {
		*out = append(*out, Diagnostic{
			Severity: DiagnosticWarning,
			Path:     path,
			Message:  "action without emitted event",
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
