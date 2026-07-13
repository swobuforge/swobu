// Package paint owns the off-screen cell buffer and scoped painter contract
// for the retained TUI rendergraph.
package paint

// Color is a TUI color value. Zero is default/unset.
type Color uint32

// ANSI16 basic color values for standard terminal palette.
const (
	ColorDefault Color = iota
	ColorBlack
	ColorRed
	ColorGreen
	ColorYellow
	ColorBlue
	ColorMagenta
	ColorCyan
	ColorWhite
)

// Style is the terminal paint style for one cell.
type Style struct {
	Fg        Color
	Bg        Color
	Bold      bool
	Dim       bool
	Underline bool
}

// IsZero returns true when no style attributes are set.
func (s Style) IsZero() bool {
	return s == Style{}
}
