package views

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/core"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/corelower"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/testharness"
)

func TestBuildCreateSectionNode_ReturnsCoreNode(t *testing.T) {
	t.Parallel()

	model := state.Model{
		CurrentEndpoint: "",
		CreateDraftName: "testworkspace",
		InteractionMode: state.InteractionModeNAV,
		CreateDraftProviderConfig: state.ProviderConfigSnapshot{
			ProviderSpec:  "openai",
			ModelID:       "gpt-4",
			BaseURL:       "https://api.openai.com",
			CredentialRef: "default",
			AuthHeader:    "Authorization",
		},
	}

	node := BuildCreateSectionNode(model)
	if diags := core.Validate(node); len(diags) > 0 {
		t.Fatalf("validation failed: %v", diags)
	}

	renderNode, err := corelower.Lower(node, corelower.EnvConfig{}, func(a state.Action) update.Action {
		return a
	})
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	if renderNode == nil {
		t.Fatal("expected render node")
	}

	tree := (&layout.TreeBuilder{}).Build(renderNode, geom.Rect{W: 80, H: 24})
	buf := paint.NewBuffer(geom.Rect{W: 80, H: 24})
	paintLayoutTree(tree, buf, &layout.PaintContext{}, geom.Point{})
	out := buf.String()

	if err := testharness.AssertVisual("create_section_ready").Viewport(80, 24).Now(out); err != nil {
		t.Fatalf("expected create section with ready status: %v\nrender=%q", err, out)
	}
}
