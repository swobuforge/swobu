// Choice option views for picker rows.
package views

import (
	"strings"

	"github.com/metrofun/swobu/internal/terminalui/engine/retained/update"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/view"
)

func NewChoiceOption[M any](label string, selected bool, onChoose func() []update.Action) view.ViewSpec[M] {
	return NewChoiceOptionWithCancel[M](label, selected, onChoose, nil)
}

func NewChoiceOptionWithCancel[M any](label string, selected bool, onChoose func() []update.Action, onCancel func() []update.Action) view.ViewSpec[M] {
	return view.View[M](func(ctx *view.Context[M]) view.RenderNode {
		return view.Materialize(ctx, ListItemRow[M](
			"- "+strings.TrimSpace(label),
			selected,
			true,
			true,
			onChoose,
			onCancel,
		))
	})
}
