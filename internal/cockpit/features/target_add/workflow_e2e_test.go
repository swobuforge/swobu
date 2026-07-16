package target_add

import (
	"context"
	"strings"
	"testing"
	"time"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/mountedrender"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

// TestWorkflow_EndToEnd walks provider → setup/loading → catalog → model → ready
// through the actionable component tree mounted by Render().
func TestWorkflow_EndToEnd(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	route := readmodel.RouteReadModel{
		ID: "gpt", ModelName: "gpt",
		Targets: []readmodel.TargetReadModel{{ID: "t1", Rank: 1, Weight: 1}},
	}
	w := NewWorkflow("dev", route, nil, nil,
		WithProviderOptions([]readmodel.ProviderOptionReadModel{
			{ProviderSpec: "openai", DisplayName: "OpenAI", SetupHint: "API key"},
			{ProviderSpec: "anthropic", DisplayName: "Anthropic", SetupHint: "API key"},
		}),
	)
	w.Open()

	rendered, err := mountedrender.String(w, 120, 40)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}

	// Phase: ChoosingProvider — provider picker should be visible.
	if w.Phase.Get() != PhaseChoosingProvider {
		t.Fatalf("phase = %v, want ChoosingProvider", w.Phase.Get())
	}
	if !strings.Contains(rendered, "provider") {
		t.Fatalf("expected 'provider' in frame:\n%s", rendered)
	}
	if !strings.Contains(rendered, "add target") {
		t.Fatalf("expected add-target title in provider picker:\n%s", rendered)
	}
	if !strings.Contains(rendered, "OpenAI") {
		t.Fatalf("expected 'OpenAI' in provider picker:\n%s", rendered)
	}
	if !strings.Contains(rendered, ">    OpenAI") {
		t.Fatalf("expected focused provider option in provider picker:\n%s", rendered)
	}
	if strings.Contains(rendered, "API key") || strings.Contains(rendered, "endpoint") {
		t.Fatalf("provider picker leaked setup detail into label-only picker:\n%s", rendered)
	}

	// Select provider via API (simulates picker activation).
	w.SelectProvider("openai")

	// Phase: ProviderSetup — ready providers surface the credential and loading rows.
	if w.Phase.Get() != PhaseProviderSetup {
		t.Fatalf("phase = %v, want ProviderSetup", w.Phase.Get())
	}
	rendered, _ = mountedrender.String(w, 120, 40)
	if !strings.Contains(rendered, "credential") {
		t.Fatalf("expected 'credential' in setup frame:\n%s", rendered)
	}
	if !strings.Contains(rendered, "new target · OpenAI") {
		t.Fatalf("expected workflow title after provider select:\n%s", rendered)
	}
	if !strings.Contains(rendered, "OpenAI") {
		t.Fatalf("expected 'OpenAI' in setup row:\n%s", rendered)
	}
	if !strings.Contains(rendered, "loading catalog…") {
		t.Fatalf("expected loading row in setup frame:\n%s", rendered)
	}

	// Activate setup confirm → enter loading, then probe catalog explicitly.
	w.ContinueSetup()
	if w.Phase.Get() != PhaseLoadingCatalog {
		t.Fatalf("phase = %v, want LoadingCatalog", w.Phase.Get())
	}
	rendered, _ = mountedrender.String(w, 120, 40)
	if !strings.Contains(rendered, "loading catalog…") {
		t.Fatalf("expected loading row while catalog is pending:\n%s", rendered)
	}
	w.ProbeCatalog()
	if w.Phase.Get() != PhaseChoosingModel {
		t.Fatalf("phase = %v, want ChoosingModel", w.Phase.Get())
	}
	rendered, _ = mountedrender.String(w, 120, 40)
	if !strings.Contains(rendered, "model") {
		t.Fatalf("expected 'model' in frame:\n%s", rendered)
	}
	if !strings.Contains(rendered, "GPT-4.1") {
		t.Fatalf("expected 'GPT-4.1' in model picker:\n%s", rendered)
	}

	// Select model via API.
	for _, d := range w.Catalog.Get().Deployments {
		if d.ID == "gpt-4.1" {
			w.SelectModel(d)
			break
		}
	}
	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("phase = %v, want ReadyToCreate", w.Phase.Get())
	}
	rendered, _ = mountedrender.String(w, 120, 40)
	if !strings.Contains(rendered, "new target · OpenAI") {
		t.Fatalf("expected ready-state workflow title:\n%s", rendered)
	}
	if !strings.Contains(rendered, ">    create") {
		t.Fatalf("expected create row focused in ready state:\n%s", rendered)
	}

	rendered, _ = mountedrender.String(w, 120, 40)
	if !strings.Contains(rendered, "provider") || !strings.Contains(rendered, "model") || !strings.Contains(rendered, "placement") || !strings.Contains(rendered, "create") {
		t.Fatalf("expected summary rows in ReadyToCreate frame:\n%s", rendered)
	}

	// Open placement picker and select balance with step 1.
	w.OpenPlacementPicker()
	if w.Phase.Get() != PhaseChoosingPlacement {
		t.Fatalf("phase = %v, want ChoosingPlacement", w.Phase.Get())
	}
	rendered, _ = mountedrender.String(w, 120, 40)
	if !strings.Contains(rendered, "balance") {
		t.Fatalf("expected 'balance' in placement picker:\n%s", rendered)
	}
	w.SelectPlacement(readmodel.PlacementOptionReadModel{
		Rank: 1, Weight: 1, Kind: readmodel.PlacementBalance,
	})
	if w.Phase.Get() != PhaseReadyToCreate {
		t.Fatalf("phase = %v, want ReadyToCreate after placement select", w.Phase.Get())
	}
	rendered, _ = mountedrender.String(w, 120, 40)
	if !strings.Contains(rendered, "balance with step 1") {
		t.Fatalf("expected selected placement summary after picker:\n%s", rendered)
	}
}

// TestWorkflow_ChatGPTAuthPendingRender proves the interactive-auth branch
// renders the pending auth rows before catalog probing begins.
func TestWorkflow_ChatGPTAuthPendingRender(t *testing.T) {
	w := NewWorkflow("dev", sampleRoute(), nil, nil)
	w.TargetAuthCommands = fakeTargetAuthCommands{
		start: func(_ context.Context, _ ports.StartAuthSessionRequest) (readmodel.AuthSessionReadModel, error) {
			return readmodel.AuthSessionReadModel{
				ProviderSpec: "chatgpt",
				SessionID:    "sess-1",
				State:        "pending",
				AuthorizeURL: "https://auth.openai.com/oauth/authorize",
				UserCode:     "ABCD-1234",
			}, nil
		},
	}

	w.SelectProvider("chatgpt")
	w.ContinueSetup()

	rendered, err := mountedrender.String(w, 120, 40)
	if err != nil {
		t.Fatalf("RenderString: %v", err)
	}
	if w.Phase.Get() != PhaseAuthPending {
		t.Fatalf("phase = %v, want AuthPending", w.Phase.Get())
	}
	if !strings.Contains(rendered, "browser login") {
		t.Fatalf("expected browser login row in frame:\n%s", rendered)
	}
	if !strings.Contains(rendered, "https://auth.openai.com/oauth/authorize") {
		t.Fatalf("expected auth URL in frame:\n%s", rendered)
	}
	if !strings.Contains(rendered, "waiting for login") {
		t.Fatalf("expected pending status row in frame:\n%s", rendered)
	}
	if !strings.Contains(rendered, "cancel") {
		t.Fatalf("expected cancel row in frame:\n%s", rendered)
	}
}

// TestWorkflow_FocusableComponentsWalk verifies that ProviderSetup and
// ChoosingModel phases render focusable action rows.
func TestWorkflow_FocusableComponentsWalk(t *testing.T) {
	w := NewWorkflow("dev", sampleRoute(), nil, nil,
		WithProviderOptions([]readmodel.ProviderOptionReadModel{
			{ProviderSpec: "openai", DisplayName: "OpenAI", SetupHint: "API key"},
		}),
	)

	h, err := testkit.NewHarness(w)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()

	h.Open()
	w.Open()

	// ChoosingProvider: SearchPicker is focusable via SelectBase.
	f1 := countFocusables(h.App().Root())

	w.SelectProvider("openai")
	h.Open()
	f2 := countFocusables(h.App().Root())
	// ProviderSetup mounts two SelectableRows.
	if f2 < f1 || f2 == 0 {
		t.Fatalf("provider setup focusables = %d, want > %d", f2, f1)
	}

	w.ContinueSetup()
	waitForWorkflowPhase(t, h, w, PhaseChoosingModel)
	f3 := countFocusables(h.App().Root())
	// ChoosingModel: SearchPicker is focusable.
	if f3 == 0 {
		t.Fatalf("model picker phase should have focusables, got %d", f3)
	}
}

func waitForWorkflowPhase(t *testing.T, h *testkit.MockAppHarness, w *Workflow, want Phase) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for w.Phase.Get() != want {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for phase %v, got %v", want, w.Phase.Get())
		}
		if !h.App().Step() {
			t.Fatalf("app stopped while waiting for phase %v", want)
		}
		time.Sleep(1 * time.Millisecond)
	}
}

func countFocusables(root *tui.Element) int {
	var count int
	root.WalkFocusables(func(f tui.Focusable) {
		count++
	})
	return count
}
