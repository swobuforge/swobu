// Package paint owns the off-screen cell buffer and scoped painter contract
// for the retained TUI rendergraph.
package paint

import (
	"strings"

	"github.com/metrofun/swobu/internal/terminalui/engine/retained/rendergraph/geom"
)

// Cell is one logical terminal cell in the engine backbuffer.
type Cell struct {
	Rune rune
}

// Painter is the scoped output contract exposed to nodes during paint.
// Implementations enforce clip/origin so paint code can stay local.
type Painter interface {
	WithOrigin(p geom.Point) Painter
	WithClip(r geom.Rect) Painter

	Put(x, y int, ch rune)
	Text(x, y int, s string)
	Fill(r geom.Rect, ch rune)
	LineH(x, y, w int, ch rune)
	LineV(x, y, h int, ch rune)
}

// BufferPainter is the off-screen cell buffer and scoped painter implementation.
type BufferPainter struct {
	bounds geom.Rect
	cells  [][]Cell
	origin geom.Point
	clip   geom.Rect
}

func NewBuffer(bounds geom.Rect) *BufferPainter {
	cells := make([][]Cell, bounds.H)
	for y := range cells {
		row := make([]Cell, bounds.W)
		for x := range row {
			row[x] = Cell{Rune: ' '}
		}
		cells[y] = row
	}
	return &BufferPainter{
		bounds: bounds,
		cells:  cells,
		clip:   bounds,
	}
}

func (b *BufferPainter) Reset() {
	for y := range b.cells {
		for x := range b.cells[y] {
			b.cells[y][x] = Cell{Rune: ' '}
		}
	}
	b.origin = geom.Point{}
	b.clip = b.bounds
}

func (b *BufferPainter) Bounds() geom.Rect { return b.bounds }

func (b *BufferPainter) Cell(x, y int) Cell {
	if y < 0 || y >= len(b.cells) || x < 0 || x >= len(b.cells[y]) {
		return Cell{Rune: ' '}
	}
	return b.cells[y][x]
}

func (b *BufferPainter) WithOrigin(p geom.Point) Painter {
	return &BufferPainter{
		bounds: b.bounds,
		cells:  b.cells,
		origin: geom.Point{X: b.origin.X + p.X, Y: b.origin.Y + p.Y},
		clip:   b.clip,
	}
}

func (b *BufferPainter) WithClip(r geom.Rect) Painter {
	return &BufferPainter{
		bounds: b.bounds,
		cells:  b.cells,
		origin: b.origin,
		clip:   b.clip.Intersect(r),
	}
}

func (b *BufferPainter) Put(x, y int, ch rune) {
	ax := b.origin.X + x
	ay := b.origin.Y + y
	if !b.clip.Contains(geom.Point{X: ax, Y: ay}) {
		return
	}
	if ay < 0 || ay >= len(b.cells) || ax < 0 || ax >= len(b.cells[ay]) {
		return
	}
	b.cells[ay][ax] = Cell{Rune: ch}
}

func (b *BufferPainter) Text(x, y int, s string) {
	col := x
	for _, r := range s {
		b.Put(col, y, r)
		col++
	}
}

func (b *BufferPainter) Fill(r geom.Rect, ch rune) {
	for y := 0; y < r.H; y++ {
		for x := 0; x < r.W; x++ {
			b.Put(r.X+x, r.Y+y, ch)
		}
	}
}

func (b *BufferPainter) LineH(x, y, w int, ch rune) {
	for i := 0; i < w; i++ {
		b.Put(x+i, y, ch)
	}
}

func (b *BufferPainter) LineV(x, y, h int, ch rune) {
	for i := 0; i < h; i++ {
		b.Put(x, y+i, ch)
	}
}

func (b *BufferPainter) String() string {
	lines := make([]string, 0, len(b.cells))
	for _, row := range b.cells {
		var sb strings.Builder
		for _, c := range row {
			r := c.Rune
			if r == 0 {
				r = ' '
			}
			sb.WriteRune(r)
		}
		lines = append(lines, strings.TrimRight(sb.String(), " "))
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}
