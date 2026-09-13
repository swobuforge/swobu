package cockpit

import (
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/mountedrender"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/testkit/cockpittestkit"
)

// TestCockpitVNextVisualGrammar is the full-shell integration fixture for the
// frozen Cockpit visual grammar. Component fixtures remain useful for individual
// states; this exact viewport prevents a locally correct accent from masking a
// broken shell, section hierarchy, or footer.
func TestCockpitVNextVisualGrammar(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	workspace := readmodel.WorkspaceReadModel{
		ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting,
		WorkspaceURL: "http://127.0.0.1:7926/c/dev",
		Routes: []readmodel.RouteReadModel{{
			ID: "gpt", ModelName: "gpt", Default: true, Enabled: true,
			Tiers: []readmodel.TierReadModel{
				{Targets: []readmodel.TargetReadModel{{ID: "gpt-4.1", Provider: "openai", Model: "gpt-4.1"}, {ID: "claude", Provider: "anthropic", Model: "claude"}}},
				{Targets: []readmodel.TargetReadModel{{ID: "gpt-4o", Provider: "openai", Model: "gpt-4o"}}},
			},
		}, {
			ID: "local", ModelName: "local", Enabled: true,
			Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{{ID: "local", Provider: "ollama", Model: "llama"}}}},
		}},
	}
	model := readmodel.CockpitReadModel{
		Tabs: []readmodel.WorkspaceTabReadModel{
			{ID: "dev", Slug: "dev", Kind: readmodel.WorkspaceTabExisting, Selected: true},
			{ID: "lab", Slug: "lab", Kind: readmodel.WorkspaceTabExisting},
			{ID: "+", Kind: readmodel.WorkspaceTabDraft},
			{ID: "?", Kind: readmodel.WorkspaceTabHelp},
		},
		SelectedWorkspaceID: "dev", SelectedWorkspace: workspace,
		Workspaces: map[readmodel.WorkspaceID]readmodel.WorkspaceReadModel{"dev": workspace},
		ActivePage: readmodel.CockpitWorkspacePage,
	}
	root := NewCockpit(model)
	root.currentWorkspacePage().RoutesSection.State.ExpandedRoute.Set("gpt")
	root.currentWorkspacePage().RoutesSection.State.FocusRoute.Set("gpt")

	screen := testkit.RenderMountedScreen(t, root, 100, 36)
	testkit.AssertVisual("vnext_visual_grammar").
		Fixture("testdata/cockpit_visual/fixture/vnext_visual_grammar_100x36.ansi").
		ExactViewport(100, 36).
		Now(t, screen)
}

func TestCockpitNoColorKeepsStructuralEmphasis(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	model := readmodel.CockpitReadModel{Tabs: []readmodel.WorkspaceTabReadModel{{ID: "dev", Slug: "dev", Selected: true}, {ID: "?", Kind: readmodel.WorkspaceTabHelp}}, SelectedWorkspaceID: "dev", SelectedWorkspace: readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting}, ActivePage: readmodel.CockpitWorkspacePage}
	app, _, err := mountedrender.NewApp(80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	configureColorCapability(app, true)
	app.SetRootComponent(NewCockpit(model))
	app.Render()
	terminal, ok := app.Terminal().(*tui.MockTerminal)
	if !ok {
		t.Fatalf("terminal = %T, want mock terminal", app.Terminal())
	}
	caps := terminal.Caps()
	for y := 0; y < 24; y++ {
		for x := 0; x < 80; x++ {
			style := terminal.CellAt(x, y).Style
			if !caps.EffectiveColor(style.Fg).IsDefault() || !caps.EffectiveColor(style.Bg).IsDefault() {
				t.Fatalf("cell %d,%d retained color under NO_COLOR: %+v", x, y, style)
			}
		}
	}
	active := terminal.CellAt(70, 0).Style
	if !active.HasAttr(tui.AttrBold | tui.AttrReverse) {
		t.Fatalf("active tab fallback = %+v, want bold+reverse", active)
	}
}
