// Section composition helpers for app views.
package views

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/view"
)

// Section composes a titled column of row views.
func Section[M any](title string, rows ...view.ViewSpec[M]) view.ViewSpec[M] {
	return view.Build[M](func(ctx *view.Context[M]) view.ViewSpec[M] {
		children := make([]view.ViewSpec[M], 0, len(rows)+1)
		children = append(children, view.Named[M]("header", sectionHeader[M](title)))
		for i, row := range rows {
			if row == nil {
				continue
			}
			children = append(children, view.Named[M](fmt.Sprintf("row/%d", i), row))
		}
		return view.VStack(ctx, children...)
	})
}

func sectionHeader[M any](title string) view.ViewSpec[M] { return NewSectionHeader[M](title) }
