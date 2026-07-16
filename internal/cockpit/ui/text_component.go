package ui

import tui "github.com/grindlemire/go-tui"

// TextComponent renders a shared non-interactive text line as a component.
// Use it for static draft headers, helper copy, and other text-only rows that
// still need to participate in templ mounting.
type TextComponent struct {
	Text string
}

// NewTextComponent builds a reusable text-only component.
func NewTextComponent(text string) *TextComponent {
	return &TextComponent{Text: text}
}

func (c *TextComponent) Render(app *tui.App) *tui.Element {
	_ = app
	return tui.New(
		tui.WithWidthPercent(100.00),
		tui.WithText(c.Text),
	)
}

var _ tui.Component = (*TextComponent)(nil)
