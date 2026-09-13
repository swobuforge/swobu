package testscreen

import (
	"bytes"
	"strings"
	"testing"
)

func TestCanonicalANSIRoundTrip(t *testing.T) {
	raw := []byte("plain \x1b[1;38;5;208;48;2;1;2;3mstyled \x1b[7m界 \x1b[0mend\nnext")
	screen, err := ParseANSI(raw)
	if err != nil {
		t.Fatal(err)
	}
	canonical := WriteANSI(screen)
	want := []byte("plain \x1b[1;38;5;208;48;2;1;2;3mstyled \x1b[0m\x1b[1;7;38;5;208;48;2;1;2;3m界 \x1b[0mend\nnext")
	if !bytes.Equal(canonical, want) {
		t.Fatalf("canonical ANSI\n got: %q\nwant: %q", canonical, want)
	}
	reparsed, err := ParseANSI(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if again := WriteANSI(reparsed); !bytes.Equal(again, canonical) {
		t.Fatalf("serialization is not stable: %q", again)
	}
}

func TestANSIProfileRejectsTerminalActivity(t *testing.T) {
	for _, raw := range []string{"a\tb", "a\rb", "\x1b[2J", "\x1b]52;c;secret\a", "\x1bPpayload\x1b\\"} {
		t.Run(strings.ReplaceAll(raw, "\x1b", "ESC"), func(t *testing.T) {
			if _, err := ParseANSI([]byte(raw)); err == nil {
				t.Fatalf("accepted forbidden control %q", raw)
			}
		})
	}
}

func TestStyledTrailingSpacesArePreserved(t *testing.T) {
	screen, err := ParseANSI([]byte("x\x1b[31m  \x1b[0m  "))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(WriteANSI(screen)), "x\x1b[31m  \x1b[0m"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestANSIColorEncodingsAreCanonical(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"ansi dark", "\x1b[31;44mx", "\x1b[31;44mx\x1b[0m"},
		{"ansi bright", "\x1b[91;104mx", "\x1b[91;104mx\x1b[0m"},
		{"ansi indexed", "\x1b[38;5;208;48;5;240mx", "\x1b[38;5;208;48;5;240mx\x1b[0m"},
		{"rgb", "\x1b[38;2;1;2;3;48;2;4;5;6mx", "\x1b[38;2;1;2;3;48;2;4;5;6mx\x1b[0m"},
		{"equivalent reset", "\x1b[31;44mx\x1b[39;49my", "\x1b[31;44mx\x1b[0my"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			screen, err := ParseANSI([]byte(test.raw))
			if err != nil {
				t.Fatal(err)
			}
			if got := string(WriteANSI(screen)); got != test.want {
				t.Fatalf("canonical ANSI = %q, want %q", got, test.want)
			}
		})
	}
}

func TestLowPackedRGBRemainsTruecolor(t *testing.T) {
	screen, err := ParseANSI([]byte("\x1b[38;2;0;0;128mx"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := screen.Cell(0, 0).Rendition.FG, RGBColor(0, 0, 128); got != want {
		t.Fatalf("low packed RGB = %+v, want %+v", got, want)
	}
	if got, want := string(WriteANSI(screen)), "\x1b[38;2;0;0;128mx\x1b[0m"; got != want {
		t.Fatalf("canonical low packed RGB = %q, want %q", got, want)
	}
}

func TestWriteANSIExactPreservesViewport(t *testing.T) {
	screen, err := ParseANSI([]byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	screen = screen.Viewport(3, 2)
	raw := WriteANSIExact(screen)
	parsed, err := ParseANSI(raw)
	if err != nil {
		t.Fatal(err)
	}
	if width, height := parsed.Size(); width != 3 || height != 2 {
		t.Fatalf("exact viewport = %dx%d, want 3x2; raw=%q", width, height, raw)
	}
}

func TestANSIAttributesAndResets(t *testing.T) {
	const all = AttrBold | AttrReverse
	screen, err := ParseANSI([]byte("\x1b[1;7ma\x1b[22;27mb"))
	if err != nil {
		t.Fatal(err)
	}
	if got := screen.Cell(0, 0).Rendition.Attrs; got != all {
		t.Fatalf("all attributes = %08b, want %08b", got, all)
	}
	if got := screen.Cell(1, 0).Rendition.Attrs; got != 0 {
		t.Fatalf("reset attributes = %08b, want 0", got)
	}
}

func TestANSIPlainTextUsesProducerNativeRuneGeometry(t *testing.T) {
	const text = "e界→"
	screen, err := ParseANSI([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if got := screen.Line(0); got != text {
		t.Fatalf("plain text = %q, want %q", got, text)
	}
	if got, want := screen.Cell(1, 0).Width, 2; got != want {
		t.Fatalf("wide-rune width = %d, want %d", got, want)
	}
	if got := string(WriteANSI(screen)); got != text {
		t.Fatalf("round trip = %q, want %q", got, text)
	}
}

func TestANSIProfileRejectsMalformedSGR(t *testing.T) {
	for _, raw := range []string{"\x1b[38;5m", "\x1b[38;5;256m", "\x1b[38;2;0;0m", "\x1b[38;2;0;0;256m", "\x1b[2m", "\x1b[3m", "\x1b[4m", "\x1b[5m", "\x1b[9m"} {
		if _, err := ParseANSI([]byte(raw)); err == nil {
			t.Errorf("accepted malformed or unsupported SGR %q", raw)
		}
	}
}

func TestANSIProfileRejectsZeroWidthRunes(t *testing.T) {
	if _, err := ParseANSI([]byte("e\u0301")); err == nil {
		t.Fatal("combining rune was promoted to an independent terminal cell")
	}
}
