package ui

import (
	"testing"

	tui "github.com/grindlemire/go-tui"
)

func TestVisualHierarchyUsesPortableForegroundAndWeight(t *testing.T) {
	muted := ToneStyle(ToneMuted)
	if muted.Fg.ANSI() != tui.BrightBlack.ANSI() || muted.HasAttr(tui.AttrDim) {
		t.Fatalf("muted style = %+v, want bright-black without dim", muted)
	}
	heading := HeadingStyle()
	if !heading.HasAttr(tui.AttrBold) || heading.HasAttr(tui.AttrDim|tui.AttrUnderline) || !heading.Fg.IsDefault() {
		t.Fatalf("heading style = %+v, want neutral bold", heading)
	}
	focus := activeFieldStyle()
	if !focus.HasAttr(tui.AttrBold) || focus.HasAttr(tui.AttrUnderline) {
		t.Fatalf("focus style = %+v, want bold without underline", focus)
	}
}

func TestActiveTabStyleCarriesCapabilityIndependentFallback(t *testing.T) {
	style := ActiveTabStyle()
	if !style.HasAttr(tui.AttrBold | tui.AttrReverse) {
		t.Fatalf("active tab style = %+v, want bold+reverse", style)
	}
	fr, fg, fb := style.Fg.RGB()
	ar, ag, ab := accentColor.RGB()
	br, bg, bb := style.Bg.RGB()
	or, og, ob := onAccentColor.RGB()
	if fr != ar || fg != ag || fb != ab || br != or || bg != og || bb != ob {
		t.Fatalf("active tab colors = fg %v bg %v, want accent/on-accent intent", style.Fg, style.Bg)
	}
}
