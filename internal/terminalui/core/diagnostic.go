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
// focusable nodes, actions, and dynamic collections. All issues are reported
// as errors; there is no warning mode. Callers must fix violations.
func Validate[E any](root Node[E]) []Diagnostic {
	var out []Diagnostic
	validateNode("root", root, &out, false)
	return out
}

var knownTokens = map[StyleToken]struct{}{
	TokenTextDefault:     {},
	TokenTextMuted:       {},
	TokenTextDanger:      {},
	TokenTextSuccess:     {},
	TokenAccentPrimary:   {},
	TokenSurfaceDefault:  {},
	TokenSurfaceSelected: {},
	TokenBorderDefault:   {},
	TokenBorderFocused:   {},
}

func validateNode[E any](path string, node Node[E], out *[]Diagnostic, requireInteractiveKeys bool) {
	if node.stateful && node.key.Empty() {
		*out = append(*out, Diagnostic{
			Severity: DiagnosticError,
			Path:     path,
			Message:  "stateful node requires key",
		})
	}
	if requireInteractiveKeys && node.key.Empty() && isInteractive[E](node) && !node.stateful {
		*out = append(*out, Diagnostic{
			Severity: DiagnosticError,
			Path:     path,
			Message:  "interactive child without key inside dynamic collection",
		})
	}

	// An action node must have a key for stable focus identity.
	if node.kind == KindAction && node.key.Empty() {
		*out = append(*out, Diagnostic{
			Severity: DiagnosticError,
			Path:     path,
			Message:  "action requires key for focusable identity",
		})
	}

	// Style token must be known if set.
	if node.style.Token != "" {
		if _, ok := knownTokens[node.style.Token]; !ok {
			*out = append(*out, Diagnostic{
				Severity: DiagnosticError,
				Path:     path,
				Message:  fmt.Sprintf("unresolved style token %q", node.style.Token),
			})
		}
	}

	// Layout dimensions must be non-negative where meaningful.
	for _, dim := range []DimSize{node.layout.Size.Width, node.layout.Size.Height} {
		switch dim.Mode {
		case DimFixed:
			if dim.Value < 0 {
				*out = append(*out, Diagnostic{
					Severity: DiagnosticError,
					Path:     path,
					Message:  fmt.Sprintf("invalid layout dimension: fixed value %d", dim.Value),
				})
			}
		case DimMinMax:
			if dim.Min < 0 {
				*out = append(*out, Diagnostic{
					Severity: DiagnosticError,
					Path:     path,
					Message:  fmt.Sprintf("invalid layout dimension: minmax min %d", dim.Min),
				})
			}
			if dim.Max < 0 {
				*out = append(*out, Diagnostic{
					Severity: DiagnosticError,
					Path:     path,
					Message:  fmt.Sprintf("invalid layout dimension: minmax max %d", dim.Max),
				})
			}
		}
	}

	// Focusable + disabled state is a mismatch.
	if node.style.State == StateDisabled && node.interaction.Focus.Mode == Focusable {
		*out = append(*out, Diagnostic{
			Severity: DiagnosticError,
			Path:     path,
			Message:  "focusable disabled mismatch: node is disabled but marked focusable",
		})
	}

	// An action node must emit at least one signal when activated.
	if node.kind == KindAction && len(node.interaction.Signals) == 0 {
		*out = append(*out, Diagnostic{
			Severity: DiagnosticError,
			Path:     path,
			Message:  "action without emitted event",
		})
	}

	// A focusable node must accept at least one intent.
	// KindInput is exempt: it receives built-in edit intents from the lowerer.
	if node.interaction.Focus.Mode == Focusable &&
		node.kind != KindInput &&
		len(node.interaction.Keymap) == 0 {
		*out = append(*out, Diagnostic{
			Severity: DiagnosticError,
			Path:     path,
			Message:  "focusable node without accepted intent",
		})
	}

	seenKeys := make(map[Key]struct{}, len(node.children))
	seenFocusIDs := make(map[FocusID]struct{}, len(node.children))
	for i, child := range node.children {
		childPath := fmt.Sprintf("%s/%d", path, i)
		if !child.key.Empty() {
			if _, ok := seenKeys[child.key]; ok {
				*out = append(*out, Diagnostic{
					Severity: DiagnosticError,
					Path:     childPath,
					Message:  fmt.Sprintf("duplicate sibling key %q", child.key),
				})
			} else {
				seenKeys[child.key] = struct{}{}
			}
		}
		fid := child.interaction.Focus.ID
		if !fid.Empty() && child.interaction.Focus.Mode == Focusable {
			if _, ok := seenFocusIDs[fid]; ok {
				*out = append(*out, Diagnostic{
					Severity: DiagnosticError,
					Path:     childPath,
					Message:  fmt.Sprintf("duplicate sibling focus ID %q", fid),
				})
			} else {
				seenFocusIDs[fid] = struct{}{}
			}
		}
		validateNode(childPath, child, out, requireInteractiveKeys || node.stateful || node.kind == KindList || node.kind == KindTable)
	}
}

// NewDiagnosticNode builds one error-node tree that renders validation
// failures inline.  Used by the lowering bridge in dev mode so an invalid
// node tree is visible as red text instead of a silent blank surface.
func NewDiagnosticNode[E any](diags []Diagnostic) Node[E] {
	children := make([]Node[E], 0, len(diags))
	for _, d := range diags {
		line := d.Path + ": " + d.Message
		children = append(children, Text[E](line).
			Style(Style{Token: TokenTextDanger, State: StateDanger}))
	}
	return Box[E](children...).
		Style(Style{Token: TokenSurfaceDefault, State: StateDanger}).
		Layout(Layout{
			Size: Size{Width: Fill(1), Height: Fit()},
		})
}

func isInteractive[E any](node Node[E]) bool {
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
