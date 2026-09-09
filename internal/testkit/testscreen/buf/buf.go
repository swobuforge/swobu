package buf

import (
	"strings"
	"unicode"
)

// View is an immutable read-only terminal screen surface.
type View interface {
	Size() (cols, rows int)
	Cell(x, y int) rune
	Line(y int) string
	String() string
}

// Matrix is a concrete read-only screen view.
type MatrixView struct {
	cols  int
	rows  int
	cells [][]rune
}

// FromString builds a screen view from raw terminal text.
func FromString(raw string) MatrixView {
	lines := parseLines(raw)
	cols := 1
	for _, line := range lines {
		if w := len([]rune(line)); w > cols {
			cols = w
		}
	}
	rows := len(lines)
	if rows < 1 {
		rows = 1
	}
	return FromStringWithViewport(raw, cols, rows)
}

// FromStringWithViewport builds a screen view using fixed viewport bounds.
func FromStringWithViewport(raw string, cols, rows int) MatrixView {
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}
	cells := make([][]rune, rows)
	for y := 0; y < rows; y++ {
		cells[y] = make([]rune, cols)
		for x := 0; x < cols; x++ {
			cells[y][x] = ' '
		}
	}
	x, y := 0, 0
	for _, r := range raw {
		switch r {
		case '\n':
			x = 0
			y++
		case '\r':
			x = 0
		default:
			if y >= rows {
				continue
			}
			if x < cols {
				cells[y][x] = r
			}
			x++
		}
		if y >= rows {
			break
		}
	}
	return MatrixView{cols: cols, rows: rows, cells: cells}
}

func (m MatrixView) Size() (cols, rows int) { return m.cols, m.rows }

func (m MatrixView) Cell(x, y int) rune {
	if y < 0 || y >= m.rows || x < 0 || x >= m.cols {
		return ' '
	}
	return m.cells[y][x]
}

func (m MatrixView) Line(y int) string {
	if y < 0 || y >= m.rows {
		return ""
	}
	return strings.TrimRightFunc(string(m.cells[y]), unicode.IsSpace)
}

func (m MatrixView) String() string {
	lines := make([]string, 0, m.rows)
	for y := 0; y < m.rows; y++ {
		lines = append(lines, m.Line(y))
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

func parseLines(screen string) []string {
	screen = strings.ReplaceAll(screen, "\r\n", "\n")
	screen = strings.TrimSuffix(screen, "\n")
	if screen == "" {
		return nil
	}
	return strings.Split(screen, "\n")
}
