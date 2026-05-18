// Inline editor views for single-line text editing.
package views

import (
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func NewInlineEditor[M any](label, value, emptyValue string, policy LineLayoutPolicy, onChange func(string) []update.Action, onCommit func(string) []update.Action, onCancel func() []update.Action) retained.ViewSpec[M] {
	return retained.View[M](func(_ *retained.Context[M]) retained.RenderNode {
		return NewInput(label, value, emptyValue, policy, onChange, onCommit, onCancel)
	})
}
