package views

import (
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/interaction"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/update"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/view"
	toolkitviews "github.com/metrofun/swobu/internal/adapters/inbound/tui/toolkit/views"
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
