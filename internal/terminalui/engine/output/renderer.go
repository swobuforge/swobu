package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/engine/model"
)

type Renderer struct {
	out        io.Writer
	reconciler model.Reconciler
	mode       model.Mode
	prev       model.Node
}

func NewRenderer(out io.Writer, mode model.Mode) *Renderer {
	if out == nil {
		out = io.Discard
	}
	return &Renderer{out: out, mode: mode}
}

func (r *Renderer) SetMode(mode model.Mode) { r.mode = mode }

func (r *Renderer) Render(next model.Node) {
	ops := r.reconciler.Reconcile(r.prev, next, r.mode)
	for _, op := range ops {
		switch op.Kind {
		case model.OpAppendLine:
			_, _ = fmt.Fprintln(r.out, op.Line)
		case model.OpUpdateStatus:
			_, _ = fmt.Fprintf(r.out, "\r%s", op.Line)
		case model.OpPaintFrame:
			_, _ = fmt.Fprintln(r.out, "\n[fullscreen]")
			_, _ = fmt.Fprintln(r.out, strings.Join(op.Lines, "\n"))
		}
	}
	r.prev = next
}
