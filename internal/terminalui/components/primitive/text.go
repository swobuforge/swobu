package primitive

import "github.com/swobuforge/swobu/internal/terminalui/core"

// Text returns one semantic text node.
func Text(value string) core.Node {
	return core.Text(value)
}

// Muted returns one muted text node.
func Muted(value string) core.Node {
	return core.Text(value).Style(core.Style{Token: core.TokenTextMuted})
}
