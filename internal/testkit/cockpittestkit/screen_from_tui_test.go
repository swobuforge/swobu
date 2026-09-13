package testkit

import (
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/testkit/testscreen"
	testscreendiff "github.com/swobuforge/swobu/internal/testkit/testscreen/diff"
)

func TestScreenFromBufferPreservesTerminalCells(t *testing.T) {
	buffer := tui.NewBuffer(4, 1)
	style := tui.NewStyle().Bold().Foreground(tui.ANSIColor(208))
	buffer.SetRune(0, 0, '界', style)
	screen, err := ScreenFromBuffer(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if got := screen.Cell(0, 0); got.Text != "界" || got.Width != 2 || got.Rendition.FG != testscreen.ANSIColor(208) || got.Rendition.Attrs&testscreen.AttrBold == 0 {
		t.Fatalf("adapted cell = %+v", got)
	}
	if got := screen.Cell(1, 0); got.Text != "" || got.Width != 0 || got.Rendition != screen.Cell(0, 0).Rendition {
		t.Fatalf("continuation cell = %+v", got)
	}
	raw := testscreen.WriteANSI(screen)
	reparsed, err := testscreen.ParseANSI(raw)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := testscreendiff.Compare(screen, reparsed, 4, 1); err != nil {
		t.Fatalf("component screen is not ANSI-round-trip stable: %v\n%s", err, result.Diff)
	}
}

func TestScreenFromBufferRejectsCombiningContent(t *testing.T) {
	buffer := tui.NewBuffer(4, 1)
	buffer.SetString(0, 0, "e\u0301", tui.NewStyle())
	if _, err := ScreenFromBuffer(buffer); err == nil {
		t.Fatal("combining content was silently accepted")
	}
}

func TestScreenFromBufferRejectsHyperlinks(t *testing.T) {
	buffer := tui.NewBuffer(4, 1)
	buffer.SetCell(0, 0, tui.Cell{Rune: 'x', Width: 1, Link: "https://example.com"})
	if _, err := ScreenFromBuffer(buffer); err == nil {
		t.Fatal("hyperlink was silently dropped")
	}
}
