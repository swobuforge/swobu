// Package inspector provides a dev-mode diagnostic overlay for core.Node trees.
// It is gated behind dev mode and must never reach production codepaths.
package inspector

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/core"
)

// Mode selects which inspector view to render.
type Mode string

const (
	ModeDiagnostics Mode = "diagnostics"
	ModeLayout      Mode = "layout"
	ModeFocus       Mode = "focus"
)

// RenderDiagnostics returns validation diagnostics as human-readable text.
func RenderDiagnostics[E any](root core.Node[E]) string {
	diags := core.Validate(root)
	if len(diags) == 0 {
		return "(no diagnostics)"
	}
	var b strings.Builder
	for _, d := range diags {
		level := "info"
		if d.Severity == core.DiagnosticError {
			level = "error"
		} else if d.Severity == core.DiagnosticWarning {
			level = "warn"
		}
		fmt.Fprintf(&b, "[%s] %s: %s\n", level, d.Path, d.Message)
	}
	return b.String()
}

// RenderLayout returns a text representation of the layout tree with
// bounding-box annotations for every node.
func RenderLayout[E any](root core.Node[E]) string {
	var b strings.Builder
	var walk func(node core.Node[E], depth int)
	walk = func(node core.Node[E], depth int) {
		indent := strings.Repeat("  ", depth)
		lay := node.LayoutValue()
		fmt.Fprintf(&b, "%s%s w=%s h=%s\n", indent, kindName(node.Kind()),
			dimString(lay.Size.Width), dimString(lay.Size.Height))
		for _, child := range node.ChildrenValue() {
			walk(child, depth+1)
		}
	}
	walk(root, 0)
	return b.String()
}

// RenderFocus returns a text representation of the semantic focus graph.
func RenderFocus[E any](root core.Node[E]) string {
	g := core.CompileFocusGraph(root)
	if g.Empty() {
		return "(no focusable nodes)"
	}
	var b strings.Builder
	var walk func(id core.FocusID, depth int)
	walk = func(id core.FocusID, depth int) {
		indent := strings.Repeat("  ", depth)
		node := g.ByID[id]
		mode := "focusable"
		if node.Mode == core.FocusGroup {
			mode = "group"
		} else if node.Mode == core.FocusScope {
			mode = "scope"
		}
		trap := ""
		if node.Trap {
			trap = " [trap]"
		}
		fmt.Fprintf(&b, "%s%s #%d (%s)%s\n", indent, id, node.Index, mode, trap)
		for _, child := range node.Children {
			walk(child, depth+1)
		}
	}
	for _, rootID := range g.Roots {
		walk(rootID, 0)
	}
	return b.String()
}

// Render returns the inspector output for the given mode.
func Render[E any](mode Mode, root core.Node[E]) string {
	switch mode {
	case ModeDiagnostics:
		return RenderDiagnostics(root)
	case ModeLayout:
		return RenderLayout(root)
	case ModeFocus:
		return RenderFocus(root)
	default:
		return "(unknown inspector mode: " + string(mode) + ")"
	}
}

func kindName(k core.Kind) string {
	switch k {
	case core.KindText:
		return "Text"
	case core.KindBox:
		return "Box"
	case core.KindStack:
		return "Stack"
	case core.KindLayer:
		return "Layer"
	case core.KindScroll:
		return "Scroll"
	case core.KindAction:
		return "Action"
	case core.KindInput:
		return "Input"
	case core.KindList:
		return "List"
	case core.KindTable:
		return "Table"
	default:
		return fmt.Sprintf("Kind(%d)", k)
	}
}

func dimString(d core.DimSize) string {
	switch d.Mode {
	case core.DimFit:
		return "fit"
	case core.DimFill:
		if d.Weight != 0 {
			return fmt.Sprintf("fill(%d)", d.Weight)
		}
		return "fill"
	case core.DimFixed:
		return fmt.Sprintf("fixed(%d)", d.Value)
	case core.DimMinMax:
		return fmt.Sprintf("minmax(%d..%d)", d.Min, d.Max)
	default:
		return fmt.Sprintf("dim(%d)", d.Mode)
	}
}
