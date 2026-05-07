package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/engine/reconcile"
	"github.com/swobuforge/swobu/internal/terminalui/view"
)

type Renderer struct {
	out        io.Writer
	reconciler reconcile.Reconciler
	mode       view.RenderMode
	prev       view.ViewSpec
	prevScene  view.Scene
}

func NewRenderer(out io.Writer, mode view.RenderMode) *Renderer {
	if out == nil {
		out = io.Discard
	}
	return &Renderer{out: out, mode: mode}
}

func (r *Renderer) SetMode(mode view.RenderMode) { r.mode = mode }

func (r *Renderer) Render(next view.ViewSpec) {
	nextN := view.Normalize(next)
	nextScene := view.Project(nextN)
	ops := r.reconciler.ReconcileScene(r.prevScene, nextScene, r.mode)
	for _, op := range ops {
		switch op.Kind {
		case reconcile.RenderOpAppendDurableLine:
			_, _ = fmt.Fprintln(r.out, op.Text)
		case reconcile.RenderOpUpdateEphemeralLine:
			_, _ = fmt.Fprintf(r.out, "\r%s", op.Text)
		case reconcile.RenderOpPaintFrame:
			_, _ = fmt.Fprintln(r.out, "\nfullscreen frame")
			_, _ = fmt.Fprintln(r.out, strings.Join(op.FrameLines, "\n"))
		}
	}
	r.prev = nextN
	r.prevScene = nextScene
}
