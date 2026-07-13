// Section composition helpers for app views.
package views

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// SectionNode composes a titled column of row core.Nodes.
func SectionNode(title string, rows ...core.Node[state.Action]) core.Node[state.Action] {
	children := make([]core.Node[state.Action], 0, len(rows)+1)
	children = append(children, core.Text[state.Action](strings.TrimSpace(title)).
		Style(core.Style{Token: core.TokenTextMuted, State: core.StateDefault}))
	for _, row := range rows {
		children = append(children, row)
	}
	return core.Stack[state.Action](core.AxisVertical, children...).
		Layout(core.Layout{
			Size: core.Size{Width: core.Fill(1), Height: core.Fit()},
			Flow: core.Flow{Mode: core.FlowStack, Axis: core.AxisVertical},
		})
}

// Section composes a titled column of row retained views.
func Section(title string, rows ...retained.ViewSpec[state.Model]) retained.ViewSpec[state.Model] {
	return retained.Build[state.Model](func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		children := make([]retained.ViewSpec[state.Model], 0, len(rows)+1)
		children = append(children, retained.Named[state.Model]("header", sectionHeader(title)))
		for i, row := range rows {
			if row == nil {
				continue
			}
			children = append(children, retained.Named[state.Model](fmt.Sprintf("row/%d", i), row))
		}
		return retained.VStack(ctx, children...)
	})
}

func sectionHeader(title string) retained.ViewSpec[state.Model] {
	title = strings.TrimSpace(title) // swobu:io-string source=boundary
	if title == "" {
		title = "section"
	}
	return NewSectionHeader(title)
}
