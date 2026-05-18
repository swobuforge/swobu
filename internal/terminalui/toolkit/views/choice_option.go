// Choice option views for picker rows.
package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func NewChoiceOption[M any](label string, selected bool, onChoose func() []update.Action) retained.ViewSpec[M] {
	return NewChoiceOptionWithCancel[M](label, selected, onChoose, nil)
}

func NewChoiceOptionWithCancel[M any](label string, selected bool, onChoose func() []update.Action, onCancel func() []update.Action) retained.ViewSpec[M] {
	return ListItemRow[M](
		InsetLabel(strings.TrimSpace(label), 3), // swobu:io-string source=boundary
		selected,
		true,
		true,
		onChoose,
		onCancel,
	)
}
