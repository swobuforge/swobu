// Section composition helpers for app views.
package views

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// Section composes a titled column of row views.
func Section[M any](title string, rows ...retained.ViewSpec[M]) retained.ViewSpec[M] {
	return retained.Build[M](func(ctx *retained.Context[M]) retained.ViewSpec[M] {
		children := make([]retained.ViewSpec[M], 0, len(rows)+1)
		children = append(children, retained.Named[M]("header", sectionHeader[M](title)))
		for i, row := range rows {
			if row == nil {
				continue
			}
			children = append(children, retained.Named[M](fmt.Sprintf("row/%d", i), row))
		}
		return retained.VStack(ctx, children...)
	})
}

func sectionHeader[M any](title string) retained.ViewSpec[M] { return NewSectionHeader[M](title) }
