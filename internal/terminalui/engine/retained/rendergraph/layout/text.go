package layout

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/view/textmetrics"
)

// TextRenderNode is the minimal intrinsic leaf node.
type TextRenderNode struct {
	Sized
	Value string
}

func NewText(s string) *TextRenderNode {
	return &TextRenderNode{
		Sized: Sized{Sizing: Sizing{W: SizeFit, H: SizeFit}},
		Value: s,
	}
}

func (t *TextRenderNode) Measure(c geom.Constraints, _ *LayoutContext) geom.Size {
	lines := strings.Split(t.Value, "\n")
	width := 0
	for _, line := range lines {
		if textmetrics.Width(line) > width {
			width = textmetrics.Width(line)
		}
	}
	return t.ResolveSize(geom.Size{W: width, H: len(lines)}, c)
}

func (t *TextRenderNode) Arrange(node *LayoutNode, _ *LayoutContext) NodeLayout {
	return NodeLayout{
		BorderRect:   node.Slot,
		ContentRect:  node.Slot,
		ViewportRect: node.Slot,
		ContentSize:  node.MeasuredSize,
	}
}

func (t *TextRenderNode) Paint(p paint.Painter, node *LayoutNode, _ *PaintContext) {
	lines := strings.Split(t.Value, "\n")
	for i, line := range lines {
		p.Text(0, i, line)
	}
	_ = node
}
