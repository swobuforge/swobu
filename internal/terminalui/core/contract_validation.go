// Contract validation cross-checks declared Node contracts against actual
// runtime state. Extracted from diagnostic.go to keep both files under the
// 400-line repo cap.
package core

import "fmt"

var validCapabilities = map[Capability]struct{}{
	CapabilityClipboard:  {},
	CapabilityBrowser:    {},
	CapabilityFilesystem: {},
	CapabilityNetwork:    {},
	CapabilityShell:      {},
	CapabilityForeground: {},
}

// validateContract cross-checks a declared contract against the actual node.
// Empty contracts are silently skipped.
func validateContract[E any](path string, node Node[E], out *[]Diagnostic) {
	c := node.contract
	if c.Name == "" && len(c.Props) == 0 && len(c.Signals) == 0 &&
		len(c.Slots) == 0 && len(c.Requires) == 0 && c.Purpose == "" {
		return
	}

	if c.Name == "" {
		*out = append(*out, Diagnostic{
			Severity: DiagnosticError,
			Path:     path,
			Message:  "contract with fields set but no name",
		})
	}

	validateContractSignals(path, c, node, out)
	validateContractProps(path, c, out)
	validateContractSlots(path, c, out)
	validateContractRequires(path, c, out)
	validateContractFocus(path, c, node, out)
	validateContractLayout(path, c, node, out)
	validateContractHelp(path, c, node, out)
}

func validateContractSignals[E any](path string, c Contract[E], node Node[E], out *[]Diagnostic) {
	seenSignalKinds := make(map[string]struct{}, len(c.Signals))
	for _, s := range c.Signals {
		if s.Kind == "" {
			*out = append(*out, Diagnostic{
				Severity: DiagnosticError,
				Path:     path,
				Message:  "contract signal with empty kind",
			})
			continue
		}
		if _, ok := seenSignalKinds[s.Kind]; ok {
			*out = append(*out, Diagnostic{
				Severity: DiagnosticError,
				Path:     path,
				Message:  fmt.Sprintf("contract duplicate signal kind %q", s.Kind),
			})
		} else {
			seenSignalKinds[s.Kind] = struct{}{}
		}
	}

	if len(c.Signals) != len(node.interaction.Signals) {
		*out = append(*out, Diagnostic{
			Severity: DiagnosticError,
			Path:     path,
			Message:  fmt.Sprintf("contract signals count %d does not match interaction signals %d", len(c.Signals), len(node.interaction.Signals)),
		})
	}
}

func validateContractProps[E any](path string, c Contract[E], out *[]Diagnostic) {
	seenPropNames := make(map[string]struct{}, len(c.Props))
	for _, p := range c.Props {
		if p.Name == "" {
			*out = append(*out, Diagnostic{
				Severity: DiagnosticError,
				Path:     path,
				Message:  "contract prop with empty name",
			})
			continue
		}
		if _, ok := seenPropNames[p.Name]; ok {
			*out = append(*out, Diagnostic{
				Severity: DiagnosticError,
				Path:     path,
				Message:  fmt.Sprintf("contract duplicate prop name %q", p.Name),
			})
		} else {
			seenPropNames[p.Name] = struct{}{}
		}
	}
}

func validateContractSlots[E any](path string, c Contract[E], out *[]Diagnostic) {
	seenSlotNames := make(map[string]struct{}, len(c.Slots))
	for _, s := range c.Slots {
		if s.Name == "" {
			*out = append(*out, Diagnostic{
				Severity: DiagnosticError,
				Path:     path,
				Message:  "contract slot with empty name",
			})
			continue
		}
		if _, ok := seenSlotNames[s.Name]; ok {
			*out = append(*out, Diagnostic{
				Severity: DiagnosticError,
				Path:     path,
				Message:  fmt.Sprintf("contract duplicate slot name %q", s.Name),
			})
		} else {
			seenSlotNames[s.Name] = struct{}{}
		}
	}
}

func validateContractRequires[E any](path string, c Contract[E], out *[]Diagnostic) {
	for _, req := range c.Requires {
		if _, ok := validCapabilities[req]; !ok {
			*out = append(*out, Diagnostic{
				Severity: DiagnosticError,
				Path:     path,
				Message:  fmt.Sprintf("contract requires unknown capability %q", req),
			})
		}
	}
}

func validateContractFocus[E any](path string, c Contract[E], node Node[E], out *[]Diagnostic) {
	focusable := node.interaction.Focus.Mode == Focusable
	if c.Focus.FocusableWhenEnabled && !focusable {
		*out = append(*out, Diagnostic{
			Severity: DiagnosticError,
			Path:     path,
			Message:  "contract claims focusable but node is not focusable",
		})
	}
	if focusable && !c.Focus.FocusableWhenEnabled && c.Name != "" {
		*out = append(*out, Diagnostic{
			Severity: DiagnosticError,
			Path:     path,
			Message:  "node is focusable but contract does not claim focusable",
		})
	}
}

func validateContractLayout[E any](path string, c Contract[E], node Node[E], out *[]Diagnostic) {
	if c.Layout.Height.Mode != 0 && c.Layout.Height.Mode != node.layout.Size.Height.Mode {
		*out = append(*out, Diagnostic{
			Severity: DiagnosticError,
			Path:     path,
			Message:  fmt.Sprintf("contract height mode %v does not match layout height mode %v", c.Layout.Height.Mode, node.layout.Size.Height.Mode),
		})
	}
	if c.Layout.Width.Mode != 0 && c.Layout.Width.Mode != node.layout.Size.Width.Mode {
		*out = append(*out, Diagnostic{
			Severity: DiagnosticError,
			Path:     path,
			Message:  fmt.Sprintf("contract width mode %v does not match layout width mode %v", c.Layout.Width.Mode, node.layout.Size.Width.Mode),
		})
	}
}

func validateContractHelp[E any](path string, c Contract[E], node Node[E], out *[]Diagnostic) {
	if len(c.Help) != len(node.interaction.Help) {
		*out = append(*out, Diagnostic{
			Severity: DiagnosticError,
			Path:     path,
			Message:  fmt.Sprintf("contract help count %d does not match interaction help %d", len(c.Help), len(node.interaction.Help)),
		})
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
