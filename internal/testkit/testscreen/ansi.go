package testscreen

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

const esc = byte(0x1b)

// ParseANSI parses the restricted static-screen profile: printable UTF-8, LF,
// and ECMA-48 SGR only.
func ParseANSI(raw []byte) (Screen, error) {
	if !utf8.Valid(raw) {
		return Screen{}, fmt.Errorf("ANSI fixture is not valid UTF-8")
	}
	if bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}) {
		return Screen{}, fmt.Errorf("ANSI fixture must not contain a BOM")
	}
	rows := [][]Cell{{}}
	rendition := Rendition{}
	for i := 0; i < len(raw); {
		switch raw[i] {
		case '\n':
			rows = append(rows, nil)
			i++
		case '\r', '\t':
			return Screen{}, fmt.Errorf("ANSI fixture contains forbidden control 0x%02x", raw[i])
		case esc:
			if i+2 >= len(raw) || raw[i+1] != '[' {
				return Screen{}, fmt.Errorf("ANSI fixture contains non-SGR escape at byte %d", i)
			}
			end := bytes.IndexByte(raw[i+2:], 'm')
			if end < 0 {
				return Screen{}, fmt.Errorf("unterminated SGR at byte %d", i)
			}
			end += i + 2
			params := string(raw[i+2 : end])
			if strings.IndexFunc(params, func(r rune) bool { return r != ';' && (r < '0' || r > '9') }) >= 0 {
				return Screen{}, fmt.Errorf("ANSI fixture contains non-SGR CSI at byte %d", i)
			}
			var err error
			rendition, err = applySGR(rendition, params)
			if err != nil {
				return Screen{}, fmt.Errorf("SGR at byte %d: %w", i, err)
			}
			i = end + 1
		default:
			r, size := utf8.DecodeRune(raw[i:])
			if r < 0x20 || r == 0x7f {
				return Screen{}, fmt.Errorf("ANSI fixture contains forbidden control 0x%02x", raw[i])
			}
			w := runewidth.RuneWidth(r)
			if w == 0 {
				return Screen{}, fmt.Errorf("ANSI fixture contains unsupported zero-width rune %q at byte %d", r, i)
			}
			if w < 0 {
				return Screen{}, fmt.Errorf("ANSI fixture contains unsupported rune %q at byte %d", r, i)
			}
			effective := EffectiveRendition(rendition)
			rows[len(rows)-1] = append(rows[len(rows)-1], Cell{Text: string(r), Width: w, Rendition: effective})
			for n := 1; n < w; n++ {
				rows[len(rows)-1] = append(rows[len(rows)-1], Cell{Width: 0, Rendition: effective})
			}
			i += size
		}
	}
	if len(rows) > 1 && len(rows[len(rows)-1]) == 0 {
		rows = rows[:len(rows)-1]
	}
	width := 1
	for _, row := range rows {
		if len(row) > width {
			width = len(row)
		}
	}
	if len(rows) == 0 {
		rows = [][]Cell{nil}
	}
	s := New(width, len(rows))
	for y, row := range rows {
		copy(s.Rows[y], row)
	}
	return s, nil
}

func WriteANSI(screen Screen) []byte {
	lastRow := -1
	for y := range screen.Rows {
		if rowEnd(screen.Rows[y]) > 0 {
			lastRow = y
		}
	}
	if lastRow < 0 {
		return nil
	}
	var out bytes.Buffer
	for y := 0; y <= lastRow; y++ {
		row := screen.Rows[y]
		end := rowEnd(row)
		current := Rendition{}
		for x := 0; x < end; {
			cell := row[x]
			if cell.Width == 0 {
				x++
				continue
			}
			if cell.Rendition != current {
				if !current.IsDefault() {
					out.WriteString("\x1b[0m")
				}
				if !cell.Rendition.IsDefault() {
					writeRendition(&out, cell.Rendition)
				}
				current = cell.Rendition
			}
			out.WriteString(cell.Text)
			x += max(cell.Width, 1)
		}
		if !current.IsDefault() {
			out.WriteString("\x1b[0m")
		}
		if y < lastRow {
			out.WriteByte('\n')
		}
	}
	return out.Bytes()
}

// WriteANSIExact serializes every cell in the supplied viewport, including
// trailing default blanks and empty rows. Exact full-frame fixtures use this
// form because terminal dimensions are part of their observable contract.
func WriteANSIExact(screen Screen) []byte {
	if screen.Width < 1 || len(screen.Rows) < 1 {
		return nil
	}
	var out bytes.Buffer
	for y := range screen.Rows {
		current := Rendition{}
		for x := 0; x < screen.Width; {
			cell := screen.Cell(x, y)
			if cell.Width == 0 {
				x++
				continue
			}
			if cell.Rendition != current {
				if !current.IsDefault() {
					out.WriteString("\x1b[0m")
				}
				if !cell.Rendition.IsDefault() {
					writeRendition(&out, cell.Rendition)
				}
				current = cell.Rendition
			}
			out.WriteString(cell.Text)
			x += max(cell.Width, 1)
		}
		if !current.IsDefault() {
			out.WriteString("\x1b[0m")
		}
		if y+1 < len(screen.Rows) {
			out.WriteByte('\n')
		}
	}
	return out.Bytes()
}

func rowEnd(row []Cell) int {
	end := len(row)
	for end > 0 {
		cell := row[end-1]
		if cell.Text != " " || cell.Width != 1 || !cell.Rendition.IsDefault() {
			break
		}
		end--
	}
	return end
}

func writeRendition(out *bytes.Buffer, r Rendition) {
	r = sgrRendition(r)
	params := make([]string, 0, 4)
	for _, attr := range []struct {
		bit  Attrs
		code string
	}{{AttrBold, "1"}, {AttrReverse, "7"}} {
		if r.Attrs&attr.bit != 0 {
			params = append(params, attr.code)
		}
	}
	params = append(params, colorParams(r.FG, false)...)
	params = append(params, colorParams(r.BG, true)...)
	out.WriteString("\x1b[" + strings.Join(params, ";") + "m")
}

func colorParams(c Color, bg bool) []string {
	base := 30
	extended := "38"
	if bg {
		base, extended = 40, "48"
	}
	switch c.Kind {
	case ColorIndexed:
		if c.Index < 8 {
			return []string{strconv.Itoa(base + int(c.Index))}
		}
		if c.Index < 16 {
			return []string{strconv.Itoa(base + 60 + int(c.Index-8))}
		}
		return []string{extended, "5", strconv.Itoa(int(c.Index))}
	case ColorRGB:
		return []string{extended, "2", strconv.Itoa(int(c.R)), strconv.Itoa(int(c.G)), strconv.Itoa(int(c.B))}
	default:
		return nil
	}
}

func applySGR(r Rendition, raw string) (Rendition, error) {
	if raw == "" {
		return Rendition{}, nil
	}
	parts := strings.Split(raw, ";")
	values := make([]int, len(parts))
	for i, part := range parts {
		if part == "" {
			values[i] = 0
			continue
		}
		v, err := strconv.Atoi(part)
		if err != nil {
			return r, err
		}
		values[i] = v
	}
	for i := 0; i < len(values); i++ {
		v := values[i]
		switch {
		case v == 0:
			r = Rendition{}
		case v == 1:
			r.Attrs |= AttrBold
		case v == 7:
			r.Attrs |= AttrReverse
		case v == 22:
			r.Attrs &^= AttrBold
		case v == 27:
			r.Attrs &^= AttrReverse
		case v >= 30 && v <= 37:
			r.FG = ANSIColor(uint8(v - 30))
		case v >= 40 && v <= 47:
			r.BG = ANSIColor(uint8(v - 40))
		case v >= 90 && v <= 97:
			r.FG = ANSIColor(uint8(v - 90 + 8))
		case v >= 100 && v <= 107:
			r.BG = ANSIColor(uint8(v - 100 + 8))
		case v == 39:
			r.FG = DefaultColor()
		case v == 49:
			r.BG = DefaultColor()
		case v == 38 || v == 48:
			color, consumed, err := parseExtendedColor(values[i+1:])
			if err != nil {
				return r, err
			}
			i += consumed
			if v == 38 {
				r.FG = color
			} else {
				r.BG = color
			}
		default:
			return r, fmt.Errorf("unsupported SGR parameter %d", v)
		}
	}
	return r, nil
}

func parseExtendedColor(values []int) (Color, int, error) {
	if len(values) >= 2 && values[0] == 5 && values[1] >= 0 && values[1] <= 255 {
		return ANSIColor(uint8(values[1])), 2, nil
	}
	if len(values) >= 4 && values[0] == 2 {
		for _, v := range values[1:4] {
			if v < 0 || v > 255 {
				return Color{}, 0, fmt.Errorf("RGB component out of range")
			}
		}
		return RGBColor(uint8(values[1]), uint8(values[2]), uint8(values[3])), 4, nil
	}
	return Color{}, 0, fmt.Errorf("invalid extended color")
}
