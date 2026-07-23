package routes

import (
	"fmt"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/mountedrender"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestRouteSectionRendersPrimaryAndFallbackTierAtCanonicalWidths(t *testing.T) {
	model := readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting, Routes: []readmodel.RouteReadModel{{ID: "chat", ModelName: "chat", Default: true, Enabled: true, Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{{ID: "a", Provider: "openai", Model: "gpt"}, {ID: "b", Provider: "anthropic", Model: "claude"}}}, {Targets: []readmodel.TargetReadModel{{ID: "c", Provider: "ollama", Model: "llama"}}}}}}}
	for _, width := range []int{80, 100, 120} {
		t.Run(string(rune(width)), func(t *testing.T) {
			section := Section(model, nil)
			section.State.ExpandedRoute.Set("chat")
			app, _, err := mountedrender.NewApp(width, 30)
			if err != nil {
				t.Fatal(err)
			}
			defer app.Close()
			app.SetRootComponent(section)
			app.Render()
			frame := app.Buffer().String()
			for _, want := range []string{"primary", "fallback 1", "openai/gpt", "ollama/llama"} {
				if !strings.Contains(frame, want) {
					t.Fatalf("width %d missing %q:\n%s", width, want, frame)
				}
			}
			if strings.Contains(frame, "step 1") {
				t.Fatalf("width %d retained rank UI:\n%s", width, frame)
			}
		})
	}
}

func TestZeroTargetExpandedRouteOmitsInapplicableDefaultRow(t *testing.T) {
	model := readmodel.WorkspaceReadModel{
		ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting,
		Routes: []readmodel.RouteReadModel{{ID: "dev", ModelName: "dev", Enabled: true}},
	}
	for _, width := range []int{80, 100, 120} {
		t.Run(fmt.Sprintf("width_%d", width), func(t *testing.T) {
			section := Section(model, nil)
			section.State.ExpandedRoute.Set("dev")
			frame := testkit.RenderMountedTrimmed(t, section, width, 20)
			if strings.Contains(frame, "target first") || strings.Contains(frame, "default           no") {
				t.Fatalf("zero-target route retained an inapplicable default row:\n%s", frame)
			}
			if !strings.Contains(frame, "add target") || !strings.Contains(frame, "add ↵") {
				t.Fatalf("zero-target route lost its next valid action:\n%s", frame)
			}
			testkit.AssertVisual("zero_target_expanded").
				Fixture(fmt.Sprintf("testdata/routes_section/fixture/zero_target_expanded_%d.txt", width)).
				Viewport(width, 20).
				Now(t, frame)
		})
	}
}

func TestRouteWithTargetKeepsDefaultAction(t *testing.T) {
	model := readmodel.WorkspaceReadModel{
		ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting,
		Routes: []readmodel.RouteReadModel{{
			ID: "dev", ModelName: "dev", Enabled: true,
			Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{{ID: "a", Provider: "openai", Model: "gpt"}}}},
		}},
	}
	section := Section(model, nil)
	section.State.ExpandedRoute.Set("dev")
	frame := testkit.RenderMountedTrimmed(t, section, 100, 20)
	if !strings.Contains(frame, "default           no") || !strings.Contains(frame, "make default ↵") {
		t.Fatalf("targeted route lost its default action:\n%s", frame)
	}
}
