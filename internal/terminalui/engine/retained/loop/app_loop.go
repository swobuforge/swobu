// AppLoop owns app model state and orchestrates reducer/effect execution,
// retained tree rebuild, focus state, and event dispatch.
package loop

import (
	"context"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/reconcile"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

// Reducer is a pure function: given the current model and one action, it
// returns zero or more outbound effects to execute asynchronously.
type Reducer[M any] func(m *M, a update.Action) []update.Effect

// AppLoop owns app model state, reducer execution, retained layout tree
// lifetime, and asynchronous effect dispatch.
type AppLoop[M any] struct {
	Model           M
	Reduce          Reducer[M]
	Tree            *layout.LayoutNode
	Focused         *layout.LayoutNode
	pendingFocusKey string
	locals          *reconcile.LocalStore
	reconciler      *reconcile.Reconciler[M]
	invalidated     bool
	followUp        chan []update.Action
	ctx             context.Context
}

func New[M any](model M, reduce Reducer[M]) *AppLoop[M] {
	locals := reconcile.NewLocalStore()
	return &AppLoop[M]{
		Model:       model,
		Reduce:      reduce,
		locals:      locals,
		reconciler:  reconcile.New[M](locals),
		invalidated: true,
		followUp:    make(chan []update.Action, 64),
	}
}
