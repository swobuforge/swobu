// Package testscreen models observable terminal cells independently of the
// library or process that produced them.
package testscreen

import (
	"strings"
	"unicode"
)

// ColorKind identifies the terminal color representation carried by Color.
type ColorKind uint8

const (
	ColorDefault ColorKind = iota
	ColorIndexed
	ColorRGB
)

// Color is either the terminal default, an ANSI palette index, or an RGB value.
type Color struct {
	Kind    ColorKind
	Index   uint8
	R, G, B uint8
}

func ANSIColor(index uint8) Color { return Color{Kind: ColorIndexed, Index: index} }

func RGBColor(r, g, b uint8) Color {
	return Color{Kind: ColorRGB, R: r, G: g, B: b}
}

func DefaultColor() Color { return Color{} }

// Attrs is the graphic-rendition attribute set used by Cockpit and preserved
// by both supported screen producers.
type Attrs uint8

const (
	AttrBold Attrs = 1 << iota
	AttrReverse
)

// Rendition is the effective SGR state of a terminal cell after terminal color
// effects such as ANSI bold brightening and reverse-video swapping. Attributes
// remain present so fixtures preserve how the effective colors are presented.
// It deliberately contains no producer-library style or application role.
type Rendition struct {
	FG, BG Color
	Attrs  Attrs
}

func (r Rendition) IsDefault() bool { return r == Rendition{} }

// EffectiveRendition canonicalizes declarative SGR intent into the effective
// cell rendition exposed by terminal emulators. ANSI foreground colors 0-7 are
// brightened by bold before reverse video swaps foreground and background.
func EffectiveRendition(r Rendition) Rendition {
	if r.Attrs&AttrBold != 0 && r.FG.Kind == ColorIndexed && r.FG.Index < 8 {
		r.FG.Index += 8
	}
	if r.Attrs&AttrReverse != 0 {
		r.FG, r.BG = r.BG, r.FG
	}
	return r
}

// sgrRendition returns the canonical SGR representative for an effective
// rendition. After undoing reverse video, bold foregrounds 8-15 use their
// base 0-7 color because terminals brighten them again; explicit bright+bold
// and base+bold are observationally indistinguishable after VT rendering.
func sgrRendition(r Rendition) Rendition {
	if r.Attrs&AttrReverse != 0 {
		r.FG, r.BG = r.BG, r.FG
	}
	if r.Attrs&AttrBold != 0 && r.FG.Kind == ColorIndexed && r.FG.Index >= 8 && r.FG.Index < 16 {
		r.FG.Index -= 8
	}
	return r
}

// Cell is one terminal grid position. A primary cell carries one producer-native
// terminal rune and its display width; continuation cells use width zero.
type Cell struct {
	Text      string
	Width     int
	Rendition Rendition
}

// Screen is normalized terminal state shared by component and PTY tests.
// Producer-owned adapters translate their native cells into this value.
type Screen struct {
	Width int
	Rows  [][]Cell
}

// New returns a default-blank screen with at least one row and column.
func New(width, height int) Screen {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	s := Screen{Width: width, Rows: make([][]Cell, height)}
	for y := range s.Rows {
		s.Rows[y] = blankRow(width)
	}
	return s
}

func (s Screen) Size() (int, int) { return s.Width, len(s.Rows) }

func (s Screen) Cell(x, y int) Cell {
	if y < 0 || y >= len(s.Rows) || x < 0 || x >= s.Width || x >= len(s.Rows[y]) {
		return blankCell()
	}
	return s.Rows[y][x]
}

func (s Screen) Line(y int) string {
	if y < 0 || y >= len(s.Rows) {
		return ""
	}
	var b strings.Builder
	for _, cell := range s.Rows[y] {
		if cell.Width != 0 {
			b.WriteString(cell.Text)
		}
	}
	return strings.TrimRightFunc(b.String(), unicode.IsSpace)
}

func (s Screen) Lines() []string {
	lines := make([]string, len(s.Rows))
	for y := range lines {
		lines[y] = s.Line(y)
	}
	return lines
}

func (s Screen) String() string {
	return strings.TrimRight(strings.Join(s.Lines(), "\n"), "\n")
}

func (s Screen) Viewport(width, height int) Screen {
	out := New(max(width, 1), max(height, 1))
	for y := 0; y < len(out.Rows) && y < len(s.Rows); y++ {
		for x := 0; x < out.Width && x < s.Width && x < len(s.Rows[y]); x++ {
			out.Rows[y][x] = s.Rows[y][x]
		}
	}
	return out
}

func blankRow(width int) []Cell {
	row := make([]Cell, width)
	for x := range row {
		row[x] = blankCell()
	}
	return row
}

func blankCell() Cell { return Cell{Text: " ", Width: 1} }
