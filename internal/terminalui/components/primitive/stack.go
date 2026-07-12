package primitive

import "github.com/swobuforge/swobu/internal/terminalui/core"

// VStack returns one vertical stack node.
func VStack(children ...core.Node) core.Node {
	return core.Stack(core.AxisVertical, children...)
}

// HStack returns one horizontal stack node.
func HStack(children ...core.Node) core.Node {
	return core.Stack(core.AxisHorizontal, children...)
}
