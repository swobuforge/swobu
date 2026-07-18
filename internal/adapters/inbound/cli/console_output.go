package cli

import (
	"io"
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"
)

// noticeBoxWidth is the fixed outer width of every console notice block. It is
// deterministic so output is terminal-independent and snapshot-testable, and
// wide enough that the longest real notice row (an install command or a bind
// error, ~54 cells) renders on a single line. At 76 columns every notice leaves
// room on an 80-column terminal.
const noticeBoxWidth = 76

// renderNoticeBlock builds a closed, rounded-corner box for a titled console
// notice: a top border carrying the title, one bordered row per input row, and
// a bottom border. It is pure and deterministic.
//
// Invariants this renderer owns:
//
//   - Width is fixed (noticeBoxWidth); the box never probes the terminal.
//   - Rows wrap to the content width. A single token wider than the content
//     width hard-breaks across lines. Rows are never ellipsized — commands and
//     errors must stay complete and legible.
//   - Control characters are sanitized: newlines/tabs become spaces and ESC
//     plus other C0 controls are dropped, so a malformed string can never break
//     the box or smuggle an escape sequence into the terminal.
//   - Display width is unicode-aware (wide CJK runes count as two cells), so
//     the right border stays aligned for non-ASCII content.
func renderNoticeBlock(title string, rows []string) []string {
	const pad = 1
	inner := noticeBoxWidth - 2 // space between the two corner glyphs
	if inner < 6 {
		inner = 6
	}
	contentWidth := inner - pad*2
	if contentWidth < 1 {
		contentWidth = 1
	}

	out := make([]string, 0, len(rows)+2)
	out = append(out, renderNoticeTop(title, inner))
	for _, row := range rows {
		cleaned := sanitizeForBox(row)
		if strings.TrimSpace(cleaned) == "" {
			out = append(out, boxRow("", pad, contentWidth))
			continue
		}
		for _, line := range wrapRow(cleaned, contentWidth) {
			out = append(out, boxRow(line, pad, contentWidth))
		}
	}
	out = append(out, "╰"+strings.Repeat("─", inner)+"╯")
	return out
}

// writeNoticeBlock renders a titled notice and writes each line with the CRLF
// line ending used across startup output.
func writeNoticeBlock(out io.Writer, title string, rows []string) {
	writeRawLines(out, renderNoticeBlock(title, rows))
}

func renderNoticeTop(title string, inner int) string {
	name := strings.TrimSpace(sanitizeForBox(title))
	if name == "" {
		name = "Notice"
	}
	label := "─ " + name + " "
	if displayWidth(label) > inner {
		limit := inner - 3 // leave room for "─ " and trailing " "
		if limit < 1 {
			limit = 1
		}
		name = truncateCells(name, limit)
		label = "─ " + name + " "
	}
	remaining := inner - displayWidth(label)
	if remaining < 0 {
		remaining = 0
	}
	return "╭" + label + strings.Repeat("─", remaining) + "╮"
}

func boxRow(content string, pad, contentWidth int) string {
	return "│" + strings.Repeat(" ", pad) + padRightCells(content, contentWidth) + strings.Repeat(" ", pad) + "│"
}

// wrapRow word-wraps s to width cells. A single word wider than width is
// hard-broken into width-sized chunks; nothing is ever truncated or ellipsized.
func wrapRow(s string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	if displayWidth(s) <= width {
		return []string{s}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	out := make([]string, 0, len(words))
	line := ""
	for _, word := range words {
		sep := ""
		if line != "" {
			sep = " "
		}
		if displayWidth(line+sep+word) <= width {
			line = line + sep + word
			continue
		}
		if line != "" {
			out = append(out, line)
			line = ""
		}
		for displayWidth(word) > width {
			chunk := truncateCells(word, width)
			out = append(out, chunk)
			word = strings.TrimPrefix(word, chunk)
		}
		line = word
	}
	out = append(out, line)
	return out
}

// truncateCells returns the longest rune-aligned prefix of s whose display width
// is at most width. It always consumes at least one rune when s is non-empty, so
// callers can hard-break even tokens whose first rune is wider than width.
func truncateCells(s string, width int) string {
	if width <= 0 {
		return ""
	}
	var b strings.Builder
	cells := 0
	for _, r := range s {
		w := runeDisplayWidth(r)
		if cells+w > width && cells > 0 {
			break
		}
		b.WriteRune(r)
		cells += w
	}
	return b.String()
}

func padRightCells(s string, width int) string {
	n := displayWidth(s)
	if n >= width {
		return truncateCells(s, width)
	}
	return s + strings.Repeat(" ", width-n)
}

func displayWidth(s string) int {
	return runewidth.StringWidth(s)
}

func runeDisplayWidth(r rune) int {
	w := runewidth.RuneWidth(r)
	if w < 0 {
		return 0
	}
	return w
}

// sanitizeForBox rewrites s so it cannot break box geometry or inject terminal
// control: newlines, carriage returns, and tabs become single spaces; ESC and
// other control runes are dropped. Everything else is preserved verbatim.
func sanitizeForBox(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteRune(' ')
		case r == 0x1b:
		case unicode.IsControl(r):
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
