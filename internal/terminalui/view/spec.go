package view

import "fmt"

// RenderMode selects how a renderer projects a view tree to terminal output.
type RenderMode string

const (
	RenderModeAppend     RenderMode = "append"
	RenderModeLive       RenderMode = "live"
	RenderModeFullscreen RenderMode = "fullscreen"
)

// Retention marks whether text is durable transcript content or ephemeral
// status content.
type Retention string

const (
	RetentionDurable   Retention = "durable"
	RetentionEphemeral Retention = "ephemeral"
)

// ViewSpec is the shared declarative terminal view tree for transcript-style
// rendering strategies. Retained interactive views live under
// internal/terminalui/view/retained and share the same root "view-first"
// namespace.
type ViewSpec struct {
	Key       string
	Kind      string
	Retention Retention
	TextNode  *TextSpec
	Panel     *PanelSpec
	Flow      *FlowSpec
	Grid      *GridSpec
	Scroll    *ScrollSpec
	Show      *ShowSpec
	Children  []ViewSpec
}

func DurableText(text string) ViewSpec {
	return ViewSpec{
		Kind:      "text",
		Retention: RetentionDurable,
		TextNode:  &TextSpec{Content: text},
	}
}

func EphemeralText(text string) ViewSpec {
	return ViewSpec{
		Kind:      "text",
		Retention: RetentionEphemeral,
		TextNode:  &TextSpec{Content: text},
	}
}

func Group(kind string, children ...ViewSpec) ViewSpec {
	return ViewSpec{Kind: kind, Children: children}
}

func FlowColumn(kind string, gap int, children ...ViewSpec) ViewSpec {
	return ViewSpec{Kind: kind, Flow: &FlowSpec{Axis: FlowAxisColumn, Gap: gap}, Children: children}
}

func FlowRow(kind string, gap int, children ...ViewSpec) ViewSpec {
	return ViewSpec{Kind: kind, Flow: &FlowSpec{Axis: FlowAxisRow, Gap: gap}, Children: children}
}

func ShowWhen(kind string, visible bool, children ...ViewSpec) ViewSpec {
	return ViewSpec{Kind: kind, Show: &ShowSpec{Visible: visible}, Children: children}
}

func GridLayout(kind string, columns int, gap int, children ...ViewSpec) ViewSpec {
	return ViewSpec{Kind: kind, Grid: &GridSpec{Columns: columns, Gap: gap}, Children: children}
}

func ScrollY(kind string, offset int, child ViewSpec) ViewSpec {
	return ViewSpec{Kind: kind, Scroll: &ScrollSpec{Axis: ScrollAxisY, Offset: offset}, Children: []ViewSpec{child}}
}

// Normalize ensures each node has a deterministic key for reconciliation.
func Normalize(root ViewSpec) ViewSpec {
	return normalize(root, "root")
}

func normalize(node ViewSpec, path string) ViewSpec {
	if node.Key == "" {
		node.Key = path
	}
	for i := range node.Children {
		childPath := fmt.Sprintf("%s/%s[%d]", node.Key, node.Children[i].Kind, i)
		node.Children[i] = normalize(node.Children[i], childPath)
	}
	return node
}
