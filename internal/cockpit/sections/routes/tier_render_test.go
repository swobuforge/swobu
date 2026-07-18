package routes

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/mountedrender"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
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
