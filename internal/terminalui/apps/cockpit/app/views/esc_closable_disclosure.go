package views

import (
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/view"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
)

// EscClosableDisclosure renders an anchored disclosure that closes on Esc when
// open, after focused descendants decline handling Esc.
func EscClosableDisclosure[M any](
	parent view.ViewSpec[M],
	open bool,
	onClose func() []update.Action,
	children ...view.ViewSpec[M],
) view.ViewSpec[M] {
	if !open {
		return parent
	}
	out := toolkitviews.NewAnchoredDisclosure(parent, children...)
	return toolkitviews.KeyScope(out, func(_ *view.Context[M], ev interaction.Event) (bool, []update.Action) {
		if ev.Kind != interaction.EventKey || ev.Key != interaction.KeyEsc {
			return false, nil
		}
		if onClose == nil {
			return true, nil
		}
		return true, onClose()
	})
}
