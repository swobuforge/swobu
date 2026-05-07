package view

import "strings"

// DurableLines returns durable transcript lines in traversal order.
func DurableLines(root ViewSpec) []string {
	return collectLines(root, RetentionDurable)
}

// EphemeralLines returns ephemeral status lines in traversal order.
func EphemeralLines(root ViewSpec) []string {
	return collectLines(root, RetentionEphemeral)
}

func collectLines(node ViewSpec, want Retention) []string {
	var walk func(ViewSpec) []string
	walk = func(n ViewSpec) []string {
		if n.Show != nil && !n.Show.Visible {
			return nil
		}
		local := make([]string, 0)
		if n.Panel != nil && n.Retention == want {
			local = append(local, renderPanelLines(*n.Panel)...)
		}
		if n.Retention == want {
			if n.TextNode != nil && n.TextNode.Content != "" {
				local = append(local, n.TextNode.Content)
			}
		}
		if n.Flow != nil {
			childBlocks := make([][]string, 0, len(n.Children))
			for _, c := range n.Children {
				childBlocks = append(childBlocks, walk(c))
			}
			local = append(local, renderFlow(*n.Flow, childBlocks)...)
			return local
		}
		if n.Grid != nil {
			childBlocks := make([][]string, 0, len(n.Children))
			for _, c := range n.Children {
				childBlocks = append(childBlocks, walk(c))
			}
			local = append(local, renderGrid(*n.Grid, childBlocks)...)
			return local
		}
		if n.Scroll != nil {
			childBlocks := make([][]string, 0, len(n.Children))
			for _, c := range n.Children {
				childBlocks = append(childBlocks, walk(c))
			}
			local = append(local, renderScroll(*n.Scroll, childBlocks)...)
			return local
		}
		for _, c := range n.Children {
			local = append(local, walk(c)...)
		}
		return local
	}
	return walk(node)
}

func renderFlow(spec FlowSpec, children [][]string) []string {
	if spec.Gap < 0 {
		spec.Gap = 0
	}
	switch spec.Axis {
	case FlowAxisRow:
		return renderFlowRow(spec.Gap, children)
	default:
		return renderFlowColumn(spec.Gap, children)
	}
}

func renderFlowColumn(gap int, children [][]string) []string {
	out := make([]string, 0)
	for i, block := range children {
		if i > 0 && gap > 0 {
			for j := 0; j < gap; j++ {
				out = append(out, "")
			}
		}
		out = append(out, block...)
	}
	return out
}

func renderFlowRow(gap int, children [][]string) []string {
	if len(children) == 0 {
		return nil
	}
	parts := make([]string, 0, len(children))
	for _, block := range children {
		line := ""
		if len(block) > 0 {
			line = block[0]
		}
		parts = append(parts, line)
	}
	return []string{strings.Join(parts, strings.Repeat(" ", gap))}
}

func renderGrid(spec GridSpec, children [][]string) []string {
	cols := spec.Columns
	if cols <= 0 {
		cols = 1
	}
	gap := spec.Gap
	if gap < 0 {
		gap = 0
	}
	out := make([]string, 0)
	for i := 0; i < len(children); i += cols {
		row := children[i:min(i+cols, len(children))]
		out = append(out, renderFlowRow(gap, row)...)
	}
	return out
}

func renderScroll(spec ScrollSpec, children [][]string) []string {
	if len(children) == 0 {
		return nil
	}
	lines := children[0]
	if spec.Axis != ScrollAxisY {
		return lines
	}
	if spec.Offset <= 0 || spec.Offset >= len(lines) {
		return lines
	}
	return lines[spec.Offset:]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
