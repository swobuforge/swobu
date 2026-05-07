package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func InsetLabel(label string, cols int) string {
	if cols < 0 {
		cols = 0
	}
	return strings.Repeat(" ", cols) + strings.TrimSpace(label)
}

// ListItemRow renders one focusable list row.
func ListItemRow[M any](
	label string,
	selected bool,
	showSelected bool,
	allowSpace bool,
	onActivate func() []update.Action,
	onCancel func() []update.Action,
) retained.ViewSpec[M] {
	return ListItemRowWithHooks[M](label, selected, showSelected, allowSpace, onActivate, onCancel, nil)
}

// ListItemRowWithHooks extends ListItemRow with focus callback.
func ListItemRowWithHooks[M any](
	label string,
	selected bool,
	showSelected bool,
	allowSpace bool,
	onActivate func() []update.Action,
	onCancel func() []update.Action,
	onFocus func() []update.Action,
) retained.ViewSpec[M] {
	return newListItemRowViewSpec(listItemRowViewSpec[M]{
		label:        label,
		selected:     selected,
		showSelected: showSelected,
		allowSpace:   allowSpace,
		onActivate:   onActivate,
		onCancel:     onCancel,
		onFocus:      onFocus,
	})
}

type listItemRowViewSpec[M any] struct {
	label        string
	selected     bool
	showSelected bool
	allowSpace   bool
	onActivate   func() []update.Action
	onCancel     func() []update.Action
	onFocus      func() []update.Action
}

func listItemRowNode[M any](w listItemRowViewSpec[M]) retained.RenderNode {
	intrinsic := runeLen(w.label) + 2 + runeLen("   selected")
	el := NewAction(intrinsic, true, w.allowSpace, func(focused bool, width int) string {
		marker := " "
		if focused {
			marker = ">"
		}
		status := ""
		if w.selected && w.showSelected {
			status = "   selected"
		}
		line := marker + " " + w.label + status
		return padRight(trimToWidthRaw(line, width), width)
	}, func(string) []update.Action {
		if w.onActivate == nil {
			return nil
		}
		return w.onActivate()
	}, w.onCancel)
	el.OnFocusAction = w.onFocus
	return el
}

func newListItemRowViewSpec[M any](w listItemRowViewSpec[M]) retained.ViewSpec[M] {
	return retained.View[M](func(_ *retained.Context[M]) retained.RenderNode {
		return listItemRowNode(w)
	})
}
