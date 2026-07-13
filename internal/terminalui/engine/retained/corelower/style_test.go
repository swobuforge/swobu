package corelower

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
)

func TestStyleResolverDefaultTokenTextDefault(t *testing.T) {
	r := StyleResolver{Palette: DefaultPalette}
	got := r.Resolve(core.Style{Token: core.TokenTextDefault})
	want := paint.Style{Fg: paint.ColorWhite}
	if got != want {
		t.Fatalf("text.default = %+v, want %+v", got, want)
	}
}

func TestStyleResolverMutedIsDim(t *testing.T) {
	r := StyleResolver{Palette: DefaultPalette}
	got := r.Resolve(core.Style{Token: core.TokenTextMuted})
	if !got.Dim {
		t.Fatal("text.muted should be dim")
	}
	if got.Fg != paint.ColorWhite {
		t.Fatalf("text.muted fg = %v, want white", got.Fg)
	}
}

func TestStyleResolverDangerToken(t *testing.T) {
	r := StyleResolver{Palette: DefaultPalette}
	got := r.Resolve(core.Style{Token: core.TokenTextDanger})
	if got.Fg != paint.ColorRed {
		t.Fatalf("text.danger fg = %v, want red", got.Fg)
	}
}

func TestStyleResolverStateOverrides(t *testing.T) {
	r := StyleResolver{Palette: DefaultPalette}

	got := r.Resolve(core.Style{Token: core.TokenSurfaceDefault, State: core.StateSelected})
	if got.Bg != paint.ColorBlue {
		t.Fatalf("selected bg = %v, want blue", got.Bg)
	}
	if !got.Bold {
		t.Fatal("selected should be bold")
	}

	got = r.Resolve(core.Style{Token: core.TokenBorderDefault, State: core.StateFocused})
	if got.Fg != paint.ColorCyan {
		t.Fatalf("focused border fg = %v, want cyan", got.Fg)
	}
	if !got.Bold {
		t.Fatal("focused should be bold")
	}
}

func TestStyleResolverModsOverride(t *testing.T) {
	r := StyleResolver{Palette: DefaultPalette}
	got := r.Resolve(core.Style{
		Token: core.TokenTextDefault,
		Mods:  core.StyleOptions{Bold: core.TriTrue, Dim: core.TriFalse},
	})
	if !got.Bold {
		t.Fatal("mods.bold=true should force bold")
	}
	if got.Dim {
		t.Fatal("mods.dim=false should force not-dim")
	}
}

func TestStyleResolverExplicitModsOverrideState(t *testing.T) {
	r := StyleResolver{Palette: DefaultPalette}
	// focused normally sets bold=true; explicit bold=false should win
	got := r.Resolve(core.Style{
		Token: core.TokenTextDefault,
		State: core.StateFocused,
		Mods:  core.StyleOptions{Bold: core.TriFalse},
	})
	if got.Bold {
		t.Fatal("explicit bold=false should override focused state bold")
	}
}
