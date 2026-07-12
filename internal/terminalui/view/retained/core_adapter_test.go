package retained

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/component"
	"github.com/swobuforge/swobu/internal/terminalui/components/compound"
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
)

func TestFromCoreRendersSemanticRows(t *testing.T) {
	t.Parallel()

	spec := FromCore(component.ViewFunc[struct{}](func(_ *component.Context[struct{}]) core.Node {
		return compound.SettingRow(compound.SettingRowProps{
			Key:         core.K("help/ask-question"),
			Label:       "ask question",
			Value:       "",
			ActionLabel: "open ↵",
			Signal:      core.SignalEvent{Kind: "cockpit.help.open", Data: struct{}{}},
			Help:        []core.HelpBindingSpec{{Key: "↵", Label: "open"}},
		})
	}))

	ctx := &Context[struct{}]{
		Local:    mapScope{m: make(map[string]any)},
		Model:    func() struct{} { return struct{}{} },
		building: true,
	}
	node := Materialize(ctx, spec)
	buf := paint.NewBuffer(geom.Rect{W: 48, H: 1})
	node.Paint(buf, &layout.LayoutNode{BorderRect: geom.Rect{W: 48, H: 1}}, &layout.PaintContext{})
	out := strings.TrimSpace(buf.String())
	if !strings.Contains(out, "ask question") {
		t.Fatalf("render = %q, want label text", out)
	}
	if !strings.Contains(out, "open ↵") {
		t.Fatalf("render = %q, want action text", out)
	}
}
