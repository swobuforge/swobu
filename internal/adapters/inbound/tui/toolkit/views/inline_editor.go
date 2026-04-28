// Inline editor views for single-line text editing.
package views

import (
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/update"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/view"
)

func NewInlineEditor[M any](label, value, emptyValue string, policy RowLayoutPolicy, onChange func(string) []update.Action, onCommit func(string) []update.Action, onCancel func() []update.Action) view.ViewSpec[M] {
	return view.View[M](func(_ *view.Context[M]) view.RenderNode {
		return NewInput(label, value, emptyValue, policy, onChange, onCommit, onCancel)
	})
}
