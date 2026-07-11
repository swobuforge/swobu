package root

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	stateeffect "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/effect"
	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/loop"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func TestRoot_RendersShellAndCanonicalSectionOrder(t *testing.T) {
	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
	})

	rt.Rebuild(Root(), geom.Rect{W: 80, H: 24})
	out := rt.Render(geom.Rect{W: 80, H: 24}).String()

	assertContainsInOrder(t, out,
		"SWOBU",
		"[› acme] [ + ]",
		"workspace",
		"routing",
		"clients",
		"traffic",
	)
	assertCockpitVocabulary(t, out)
}

func TestRoot_OnMountStartsDaemonRefreshLoop(t *testing.T) {
	t.Parallel()

	effects := rootOnMountEffects()
	if len(effects) != 4 {
		t.Fatalf("on-mount effect count = %d, want 4", len(effects))
	}
	if _, ok := effects[3].(state.ScheduleDaemonRefreshEffect); !ok {
		t.Fatalf("on-mount effect[3] = %T, want state.ScheduleDaemonRefreshEffect", effects[3])
	}
}

func TestRoot_RendersMinimumViewportMessageBelow60x18(t *testing.T) {
	rt := newTestRuntime(state.Model{})
	rt.Rebuild(Root(), geom.Rect{W: 40, H: 12})
	out := rt.Render(geom.Rect{W: 40, H: 12}).String()
	if !strings.Contains(out, "Terminal too small") {
		t.Fatalf("render = %q, want minimum viewport message", out)
	}
}

func TestRoot_SmallViewportGuardAppliesAfterBodyFocusTraversal(t *testing.T) {
	normal := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
	})
	normalViewport := geom.Rect{W: 80, H: 24}
	normal.Rebuild(Root(), normalViewport)
	focusRowContaining(t, normal, normalViewport, "endpoint")
	normal.DispatchEvent(updateKey(interaction.KeyDown))
	normal.DispatchEvent(updateKey(interaction.KeyDown))
	normal.Rebuild(Root(), normalViewport)
	_ = normal.Render(normalViewport).String()

	small := newTestRuntime(state.Model{})
	smallViewport := geom.Rect{W: 40, H: 12}
	small.Rebuild(Root(), smallViewport)
	out := small.Render(smallViewport).String()
	if !strings.Contains(out, "Terminal too small") {
		t.Fatalf("render = %q, want minimum viewport message after focus traversal", out)
	}
}

func TestRoot_FirstBuildShowsOneFocusableCursor(t *testing.T) {
	rt := newTestRuntime(state.Model{
		HeaderStatus: "ready",
		DaemonState:  "up",
	})
	viewport := geom.Rect{W: 80, H: 24}
	rt.Rebuild(Root(), viewport)
	out := rt.Render(viewport).String()
	if !strings.Contains(out, ">") {
		t.Fatalf("render missing focused cursor marker after first rebuild: %q", out)
	}
}

func TestRoot_WorkspaceRailSelectsEndpointsAndCreateLaneViaTabOnly(t *testing.T) {
	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme", "staging"},
		CurrentEndpoint: "acme",
	})

	rt.Rebuild(Root(), geom.Rect{W: 80, H: 24})
	rt.DispatchEvent(updateKey(interaction.KeyTab))
	rt.Rebuild(Root(), geom.Rect{W: 80, H: 24})
	if got := rt.Model.CurrentEndpoint; got != "staging" {
		t.Fatalf("current endpoint after tab = %q, want staging", got)
	}
	if out := rt.Render(geom.Rect{W: 80, H: 24}).String(); !strings.Contains(out, "[› staging]") {
		t.Fatalf("render missing selected staging rail tab: %q", out)
	}

	rt.DispatchEvent(updateKey(interaction.KeyTab))
	rt.Rebuild(Root(), geom.Rect{W: 80, H: 24})
	if got := rt.Model.CurrentEndpoint; got != "" {
		t.Fatalf("current endpoint after second tab = %q, want empty create lane", got)
	}
	if out := rt.Render(geom.Rect{W: 80, H: 24}).String(); !strings.Contains(out, "[› +]") {
		t.Fatalf("render missing selected create rail tab: %q", out)
	}
}

func TestRoot_TabCyclesWorkspaceRailFromBodyFocus(t *testing.T) {
	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme", "staging"},
		CurrentEndpoint: "acme",
	})
	viewport := geom.Rect{W: 80, H: 24}
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "name")
	rt.DispatchEvent(updateKey(interaction.KeyTab))
	rt.Rebuild(Root(), viewport)
	if got := rt.Model.CurrentEndpoint; got != "staging" {
		t.Fatalf("current endpoint after tab = %q, want staging", got)
	}
	rt.DispatchEvent(updateKey(interaction.KeyShiftTab))
	rt.Rebuild(Root(), viewport)
	if got := rt.Model.CurrentEndpoint; got != "acme" {
		t.Fatalf("current endpoint after shift+tab = %q, want acme", got)
	}
}

func TestRoot_WorkspaceSwitchResetsWorkspaceLocalClientsState(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme", "staging"},
		CurrentEndpoint: "acme",
		EndpointSnapshots: []state.EndpointSnapshot{
			{Name: "acme"},
			{Name: "staging"},
		},
	})
	viewport := geom.Rect{W: 120, H: 30}
	rt.Rebuild(Root(), viewport)

	selectClientFromChooser(t, rt, viewport, "Codex")
	focusRowContaining(t, rt, viewport, "client             Codex")

	rt.DispatchEvent(updateKey(interaction.KeyTab))
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "clients")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "client            ")
	out := rt.Render(viewport).String()
	if !strings.Contains(out, "client             not set") {
		t.Fatalf("expected clients local state reset after workspace switch; render=%q", out)
	}
}

func TestRoot_ClientsSectionOpenFocusesClientRowByKey(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
		EndpointSnapshots: []state.EndpointSnapshot{
			{Name: "acme"},
		},
	})
	viewport := geom.Rect{W: 100, H: 28}
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "clients")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	if rt.Focused == nil {
		t.Fatal("expected focused client row after opening clients section")
	}
	_, key, _ := retained.NamedNodeMetadata(layout.UnwrapIdentity(rt.Focused.RenderNode))
	if key != "client" {
		t.Fatalf("focused key = %q, want client", key)
	}
}

func TestRoot_EscOnOpenRoutingSectionCollapsesSectionBeforeExit(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		CreateDraftName: "acme",
		CreateDraftProviderConfig: state.ProviderConfigSnapshot{
			Ref:              state.DraftProviderRef,
			ProviderSpec:     "openai",
			ProviderProtocol: "auto",
			CredentialRef:    "env:OPENAI_API_KEY",
			ModelID:          "gpt-5.3",
		},
	})
	viewport := geom.Rect{W: 80, H: 24}
	rt.Rebuild(Root(), viewport)

	out := rt.Render(viewport).String()
	if strings.Contains(out, "provider           ") {
		t.Fatalf("routing should start collapsed instead of opening from populated draft state; render=%q", out)
	}

	focusRowContaining(t, rt, viewport, "routing")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "provider")

	rt.DispatchEvent(updateKey(interaction.KeyEsc))
	rt.Rebuild(Root(), viewport)

	out = rt.Render(viewport).String()
	assertRootScenario(t, out, "routing_collapsed_by_esc")
}

func TestRoot_ClientActionPayloadDisclosure_CopyRevealsOnActivateOnly(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
		EndpointSnapshots: []state.EndpointSnapshot{
			{Name: "acme"},
		},
	})
	viewport := geom.Rect{W: 120, H: 30}
	rt.Rebuild(Root(), viewport)

	selectClientFromChooser(t, rt, viewport, "Codex")
	focusRowContaining(t, rt, viewport, "file config")
	out := rt.Render(viewport).String()
	if strings.Contains(out, `model_provider = "swobu"`) {
		t.Fatalf("file-config payload should stay hidden on focus; render=%q", out)
	}

	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	out = rt.Render(viewport).String()
	if !strings.Contains(out, `model_provider = "swobu"`) || !strings.Contains(out, `base_url = "http://127.0.0.1:7926/c/acme/v1"`) {
		t.Fatalf("file-config payload should be visible after activate; render=%q", out)
	}
}

func TestRoot_ClientActionPayloadDisclosure_OpenCodeFileConfigScrollsAndPreservesBodyNav(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
		EndpointSnapshots: []state.EndpointSnapshot{
			{Name: "acme"},
		},
	})
	viewport := geom.Rect{W: 120, H: 30}
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "clients")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "client            ")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	for i := 0; i < 4; i++ {
		rt.DispatchEvent(updateKey(interaction.KeyDown))
		rt.Rebuild(Root(), viewport)
	}
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	focusRowContaining(t, rt, viewport, "file config")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	out := rt.Render(viewport).String()
	if strings.Contains(out, `"baseURL": "http://127.0.0.1:7926/c/acme/v1"`) {
		t.Fatalf("expected OpenCode baseURL to be below initial disclosure viewport before scrolling; render=%q", out)
	}
	for i := 0; i < 8; i++ {
		rt.DispatchEvent(updateKey(interaction.KeyDown))
		rt.Rebuild(Root(), viewport)
	}
	out = rt.Render(viewport).String()
	if !strings.Contains(out, `"baseURL":`) || !strings.Contains(out, `"http://127.0.0.1:7926/c/acme/v1"`) {
		t.Fatalf("expected OpenCode baseURL visible after disclosure scrolling; render=%q", out)
	}

	focusedRun := false
	for i := 0; i < 80; i++ {
		out = rt.Render(viewport).String()
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, ">") && strings.Contains(line, "run                command") {
				focusedRun = true
				break
			}
		}
		if focusedRun {
			break
		}
		rt.DispatchEvent(updateKey(interaction.KeyDown))
		rt.Rebuild(Root(), viewport)
	}
	if !focusedRun {
		t.Fatalf("expected focus to move to run row after disclosure scrolling; render=%q", rt.Render(viewport).String())
	}
}

func TestRoot_OpenCodePayloadKeepsFooterVisibleInCompactViewport(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"test"},
		CurrentEndpoint: "test",
		EndpointSnapshots: []state.EndpointSnapshot{
			{Name: "test"},
		},
	})
	viewport := geom.Rect{W: 80, H: 24}
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "clients")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	focusRowContaining(t, rt, viewport, "client            ")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	for i := 0; i < 4; i++ {
		rt.DispatchEvent(updateKey(interaction.KeyDown))
		rt.Rebuild(Root(), viewport)
	}
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	focusRowContaining(t, rt, viewport, "file config")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	out := rt.Render(viewport).String()
	if !strings.Contains(out, "↑↓ move") || !strings.Contains(out, "tab tabs") {
		t.Fatalf("expected footer hints visible in compact viewport during long payload disclosure; render=%q", out)
	}
	if !strings.Contains(out, "⛉ SWOBU") {
		t.Fatalf("expected header rail visible in compact viewport during long payload disclosure; render=%q", out)
	}
}

func TestRoot_FirstRunBedrockCreateFlow_RequiresScopeBeforeModel(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		CreateDraftName: "test",
		CreateDraftProviderConfig: state.ProviderConfigSnapshot{
			Ref:           state.DraftProviderRef,
			ProviderSpec:  "bedrock",
			CredentialRef: "profile:default",
			BaseURL:       "",
			ModelID:       "",
		},
	})
	viewport := geom.Rect{W: 100, H: 26}
	rt.Rebuild(Root(), viewport)
	openRoutingSection(t, rt, viewport)
	out := rt.Render(viewport).String()

	// Canonical slot grammar must remain stable for Bedrock in first-run.
	assertContainsInOrder(t, out, "provider", "credential", "region", "model", "protocol", "create")
	// Model must remain blocked until scope is explicit.
	if !strings.Contains(out, "model") || !strings.Contains(strings.ToLower(out), "blocked") {
		t.Fatalf("bedrock first-run model should be blocked before scope is resolved; render=%q", out)
	}
}

func TestRoot_FirstRunBedrockCreateFlow_DoesNotSilentlyDefaultScope(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		CreateDraftName: "test",
		CreateDraftProviderConfig: state.ProviderConfigSnapshot{
			Ref:           state.DraftProviderRef,
			ProviderSpec:  "bedrock",
			CredentialRef: "profile:default",
			BaseURL:       "",
			ModelID:       "",
		},
	})
	viewport := geom.Rect{W: 100, H: 26}
	rt.Rebuild(Root(), viewport)
	openRoutingSection(t, rt, viewport)
	out := strings.ToLower(rt.Render(viewport).String())

	// Missing scope must render as missing; default region inference is invalid in this state.
	if strings.Contains(out, "eu-west-2") || strings.Contains(out, "us-east-1") {
		t.Fatalf("bedrock first-run region default leaked into missing-region state; render=%q", out)
	}
	if !(strings.Contains(out, "region") && strings.Contains(out, "missing")) {
		t.Fatalf("bedrock first-run missing region should be explicit; render=%q", out)
	}
}

func TestRoot_FirstRunBedrockCreateFlow_DerivesRegionFromEnvWhenURLMissing(t *testing.T) {
	t.Setenv("AWS_REGION", "eu-west-2")
	t.Setenv("AWS_DEFAULT_REGION", "")

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		CreateDraftName: "test",
		CreateDraftProviderConfig: state.ProviderConfigSnapshot{
			Ref:           state.DraftProviderRef,
			ProviderSpec:  "bedrock",
			CredentialRef: "profile:default",
			BaseURL:       "",
			ModelID:       "",
		},
	})
	viewport := geom.Rect{W: 100, H: 26}
	rt.Rebuild(Root(), viewport)
	openRoutingSection(t, rt, viewport)
	out := rt.Render(viewport).String()
	assertRootScenario(t, out, "bedrock_region_derived_from_env")
}

func TestRoot_FirstRunBedrockCreateFlow_NoAWSProfilesFound_ShowsExplicitProfileState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_REGION", "eu-west-1")
	t.Setenv("AWS_DEFAULT_REGION", "")

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		CreateDraftName: "test",
		CreateDraftProviderConfig: state.ProviderConfigSnapshot{
			Ref:           state.DraftProviderRef,
			ProviderSpec:  "bedrock",
			CredentialRef: "aws_env_session",
			BaseURL:       "",
			ModelID:       "",
		},
	})
	viewport := geom.Rect{W: 100, H: 26}
	rt.Rebuild(Root(), viewport)
	openRoutingSection(t, rt, viewport)
	out := rt.Render(viewport).String()
	assertRootScenario(t, out, "bedrock_no_aws_profiles_found")
}

func TestFirstRunPrimaryEnterActions_ReadyRoutingFocusDispatchesCreate(t *testing.T) {
	actions, ok := firstRunPrimaryEnterActions(state.Model{
		FooterVerb:      "create",
		FooterBaseVerb:  "next",
		CreateDraftName: "jobs",
		CreateDraftProviderConfig: state.ProviderConfigSnapshot{
			ProviderSpec:  "openai",
			CredentialRef: "env:OPENAI_API_KEY",
			ModelID:       "gpt-4.1-mini",
		},
	})
	if !ok {
		t.Fatalf("ok=false, want true")
	}
	if len(actions) != 2 {
		t.Fatalf("actions=%d want=2", len(actions))
	}
	if _, ok := actions[1].(state.WorkspaceCreateRequested); !ok {
		t.Fatalf("action[1]=%T want WorkspaceCreateRequested", actions[1])
	}
}

func TestFirstRunPrimaryEnterActions_CreateRowFocusDoesNotOverrideLocalAction(t *testing.T) {
	if _, ok := firstRunPrimaryEnterActions(state.Model{
		FooterVerb:      "create",
		FooterBaseVerb:  "create",
		CreateDraftName: "jobs",
		CreateDraftProviderConfig: state.ProviderConfigSnapshot{
			ProviderSpec:  "openai",
			CredentialRef: "env:OPENAI_API_KEY",
			ModelID:       "gpt-4.1-mini",
		},
	}); ok {
		t.Fatalf("ok=true, want false")
	}
}

func TestRoot_FirstRunOllamaHidesCredentialRowWhenExternalAndNonSelectable(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		CreateDraftName: "test",
		CreateDraftProviderConfig: state.ProviderConfigSnapshot{
			Ref:          state.DraftProviderRef,
			ProviderSpec: "ollama",
			ModelID:      "",
		},
	})
	viewport := geom.Rect{W: 100, H: 26}
	rt.Rebuild(Root(), viewport)
	openRoutingSection(t, rt, viewport)
	out := rt.Render(viewport).String()
	if strings.Contains(strings.ToLower(out), "credential") {
		t.Fatalf("ollama first-run should hide credential row when external/non-selectable; render=%q", out)
	}
}

func TestRoot_FirstRunBedrockShowsCredentialRowForStrategySelection(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		CreateDraftName: "test",
		CreateDraftProviderConfig: state.ProviderConfigSnapshot{
			Ref:          state.DraftProviderRef,
			ProviderSpec: "bedrock",
		},
	})
	viewport := geom.Rect{W: 100, H: 26}
	rt.Rebuild(Root(), viewport)
	openRoutingSection(t, rt, viewport)
	out := strings.ToLower(rt.Render(viewport).String())
	if !strings.Contains(out, "credential") {
		t.Fatalf("bedrock first-run should show credential row for strategy selection; render=%q", out)
	}
}

func TestRoot_OpenCodePayloadShowsScrollAffordanceCues(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"test"},
		CurrentEndpoint: "test",
		EndpointSnapshots: []state.EndpointSnapshot{
			{Name: "test"},
		},
	})
	viewport := geom.Rect{W: 80, H: 24}
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "clients")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	focusRowContaining(t, rt, viewport, "client            ")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	for i := 0; i < 4; i++ {
		rt.DispatchEvent(updateKey(interaction.KeyDown))
		rt.Rebuild(Root(), viewport)
	}
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	focusRowContaining(t, rt, viewport, "file config")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	out := rt.Render(viewport).String()
	if !strings.Contains(out, "↓ more") {
		t.Fatalf("expected downward disclosure affordance at top of OpenCode payload; render=%q", out)
	}
	if strings.Contains(out, "↑ more") {
		t.Fatalf("unexpected upward disclosure affordance before payload scroll; render=%q", out)
	}

	for i := 0; i < 8; i++ {
		rt.DispatchEvent(updateKey(interaction.KeyDown))
		rt.Rebuild(Root(), viewport)
	}
	out = rt.Render(viewport).String()
	if !strings.Contains(out, "↑ more") {
		t.Fatalf("expected upward disclosure affordance after payload scroll; render=%q", out)
	}
}

func TestRoot_ClientPickerKeepsFocusedChoiceVisibleInCompactViewport(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"test"},
		CurrentEndpoint: "test",
		EndpointSnapshots: []state.EndpointSnapshot{
			{Name: "test"},
		},
	})
	viewport := geom.Rect{W: 80, H: 24}
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "clients")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	focusRowContaining(t, rt, viewport, "client            ")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	for i := 0; i < 5; i++ {
		rt.DispatchEvent(updateKey(interaction.KeyDown))
		rt.Rebuild(Root(), viewport)
	}
	out := rt.Render(viewport).String()
	if !strings.Contains(out, ">     Other (Cline, Roo Code, OpenClaw, etc)") {
		t.Fatalf("expected focused picker option to remain visible while navigating compact picker; render=%q", out)
	}
}

func TestRoot_ClientsSectionReachableFromLocalDisclosureFocusStates(t *testing.T) {
	t.Parallel()

	viewport := geom.Rect{W: 100, H: 28}
	seeds := []struct {
		name string
		seed func(t *testing.T, rt *loop.AppLoop[state.Model], viewport geom.Rect)
	}{
		{
			name: "client-chooser-open",
			seed: func(t *testing.T, rt *loop.AppLoop[state.Model], viewport geom.Rect) {
				focusRowContaining(t, rt, viewport, "clients")
				rt.DispatchEvent(updateKey(interaction.KeyEnter))
				rt.Rebuild(Root(), viewport)
				focusRowContaining(t, rt, viewport, "client            ")
				rt.DispatchEvent(updateKey(interaction.KeyEnter))
				rt.Rebuild(Root(), viewport)
				focusChooserOptionContaining(t, rt, viewport, "Codex")
			},
		},
		{
			name: "client-action-disclosure-open",
			seed: func(t *testing.T, rt *loop.AppLoop[state.Model], viewport geom.Rect) {
				selectClientFromChooser(t, rt, viewport, "Codex")
				focusRowContaining(t, rt, viewport, "file config")
				rt.DispatchEvent(updateKey(interaction.KeyEnter))
				rt.Rebuild(Root(), viewport)
			},
		},
	}

	for _, tc := range seeds {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			rt := newTestRuntime(state.Model{
				HeaderStatus:    "ready",
				DaemonState:     "up",
				CreateDraftName: "acme",
				Endpoints:       []string{"acme"},
				CurrentEndpoint: "acme",
				EndpointSnapshots: []state.EndpointSnapshot{
					{Name: "acme"},
				},
			})
			rt.Rebuild(Root(), viewport)
			tc.seed(t, rt, viewport)

			ensureClientsSectionOpenFromAnyFocusState(t, rt, viewport)
		})
	}
}

func TestRoot_ClientActionPayloadDisclosure_ManualRunRevealsOnActivateOnly(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
		EndpointSnapshots: []state.EndpointSnapshot{
			{Name: "acme"},
		},
	})
	viewport := geom.Rect{W: 120, H: 30}
	rt.Rebuild(Root(), viewport)

	selectClientFromChooser(t, rt, viewport, "Codex")
	focusRowContaining(t, rt, viewport, "run")
	out := rt.Render(viewport).String()
	if strings.Contains(out, `model_providers.swobu.base_url="http://127.0.0.1:7926/c/acme/v1"`) {
		t.Fatalf("run command payload should stay hidden on focus; render=%q", out)
	}

	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	out = rt.Render(viewport).String()
	if !strings.Contains(out, `model_provider`) || !strings.Contains(out, `model_providers.swobu.base_url`) {
		t.Fatalf("run command payload should be visible after activate; render=%q", out)
	}
}

func TestRoot_EscClosesAddModelProviderDrawer(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
		EndpointSnapshots: []state.EndpointSnapshot{
			{
				Name:                      "acme",
				SelectedProviderConfigRef: "ollama:gemma4",
				ProviderConfigs: []state.ProviderConfigSnapshot{
					{
						Ref:              "ollama:gemma4",
						ProviderSpec:     "ollama",
						ModelID:          "gemma4:e4b",
						ProviderProtocol: "auto",
						CredentialRef:    "",
					},
				},
			},
		},
	})
	viewport := geom.Rect{W: 100, H: 30}
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "routing")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	if !strings.Contains(rt.Render(viewport).String(), "models") {
		focusRowContaining(t, rt, viewport, "routing")
		rt.DispatchEvent(updateKey(interaction.KeyEnter))
		rt.Rebuild(Root(), viewport)
	}
	focusRowContaining(t, rt, viewport, "models")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "add model")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "provider")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	if out := rt.Render(viewport).String(); !strings.Contains(out, "OpenRouter") {
		t.Fatalf("expected provider drawer options visible before esc; render=%q", out)
	}

	rt.DispatchEvent(updateKey(interaction.KeyEsc))
	rt.Rebuild(Root(), viewport)

	out := rt.Render(viewport).String()
	if strings.Contains(out, "OpenRouter") {
		t.Fatalf("expected esc to close provider drawer options; render=%q", out)
	}
}

func TestRoot_WorkspaceAddModelSelectingFileCredentialShowsFileRow(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
		EndpointSnapshots: []state.EndpointSnapshot{
			{
				Name:                      "acme",
				SelectedProviderConfigRef: "ollama:gemma4",
				ProviderConfigs: []state.ProviderConfigSnapshot{
					{
						Ref:              "ollama:gemma4",
						ProviderSpec:     "ollama",
						ModelID:          "gemma4:e4b",
						ProviderProtocol: "auto",
					},
				},
			},
		},
	})
	viewport := geom.Rect{W: 110, H: 32}
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "routing")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	focusRowContaining(t, rt, viewport, "models")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "add model")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "provider")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	focusChooserOptionContaining(t, rt, viewport, "OpenAI")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	selectAddModelFileCredential(t, rt, viewport)
	out := rt.Render(viewport).String()
	assertRootScenario(t, out, "file_auth_blocked")
}

func TestRoot_WorkspaceAddModelCredentialSourceToggleDoesNotPanicAndKeepsRowsCoherent(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
		EndpointSnapshots: []state.EndpointSnapshot{
			{
				Name:                      "acme",
				SelectedProviderConfigRef: "ollama:gemma4",
				ProviderConfigs: []state.ProviderConfigSnapshot{
					{
						Ref:              "ollama:gemma4",
						ProviderSpec:     "ollama",
						ModelID:          "gemma4:e4b",
						ProviderProtocol: "auto",
					},
				},
			},
		},
	})
	viewport := geom.Rect{W: 110, H: 32}
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "routing")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	focusRowContaining(t, rt, viewport, "models")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "add model")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "provider")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	focusChooserOptionContaining(t, rt, viewport, "OpenAI")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	chooseCredential := func(option string) string {
		if strings.Contains(rt.Render(viewport).String(), "↵ save") {
			rt.DispatchEvent(updateKey(interaction.KeyEsc))
			rt.Rebuild(Root(), viewport)
		}
		focusRowContaining(t, rt, viewport, "add model")
		rt.DispatchEvent(updateKey(interaction.KeyDown))
		rt.Rebuild(Root(), viewport)
		rt.DispatchEvent(updateKey(interaction.KeyDown))
		rt.Rebuild(Root(), viewport)
		rt.DispatchEvent(updateKey(interaction.KeyEnter))
		rt.Rebuild(Root(), viewport)
		focusChooserOptionContaining(t, rt, viewport, option)
		rt.DispatchEvent(updateKey(interaction.KeyEnter))
		rt.Rebuild(Root(), viewport)
		return rt.Render(viewport).String()
	}

	out := chooseCredential("env")
	assertRootScenario(t, out, "env_selected")

	out = chooseCredential("file")
	assertRootScenario(t, out, "file_selected")

	out = chooseCredential("env")
	assertRootScenario(t, out, "env_reselected")
}

func TestRoot_RoutingModelsDrawerGrammarAligned(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
		EndpointSnapshots: []state.EndpointSnapshot{
			{
				Name:                      "acme",
				SelectedProviderConfigRef: "openai:gpt-5.3",
				ProviderConfigs: []state.ProviderConfigSnapshot{
					{
						Ref:              "openai:gpt-5.3",
						ProviderSpec:     "openai",
						ModelID:          "gpt-5.3",
						ProviderProtocol: "auto",
						CredentialRef:    "env:OPENAI_API_KEY",
					},
					{
						Ref:              "anthropic:opus",
						ProviderSpec:     "anthropic",
						ModelID:          "opus",
						ProviderProtocol: "auto",
						CredentialRef:    "env:ANTHROPIC_API_KEY",
					},
				},
			},
		},
	})
	viewport := geom.Rect{W: 100, H: 30}
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "routing")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	focusRowContaining(t, rt, viewport, "provider")

	focusRowContaining(t, rt, viewport, "models")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	out := rt.Render(viewport).String()
	if !strings.Contains(out, "gpt-5.3") || !strings.Contains(out, "opus") {
		t.Fatalf("expected concise model rows in models drawer; render=%q", out)
	}
	if strings.Contains(out, "provider:") || strings.Contains(out, "selected") {
		t.Fatalf("unexpected stale summary clutter in model rows; render=%q", out)
	}

	focusRowContaining(t, rt, viewport, "add model")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	out = rt.Render(viewport).String()
	assertRootScenario(t, out, "add_model_open")
}

func TestRoot_RoutingAliasEditsInline(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
		EndpointSnapshots: []state.EndpointSnapshot{
			{
				Name:                      "acme",
				SelectedProviderConfigRef: "chatgpt:gpt-5.3-codex:model-1",
				ProviderConfigs: []state.ProviderConfigSnapshot{
					{
						Ref:              "chatgpt:gpt-5.3-codex:model-1",
						ProviderSpec:     "chatgpt",
						ModelID:          "gpt-5.3-codex",
						ProviderProtocol: "auto",
						CredentialRef:    "chatgpt_login",
					},
				},
			},
		},
	})
	viewport := geom.Rect{W: 100, H: 30}
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "routing")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "models")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	out := rt.Render(viewport).String()
	assertRootScenario(t, out, "alias_row_closed")

	focusRowContaining(t, rt, viewport, "alias")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	out = rt.Render(viewport).String()
	assertRootScenario(t, out, "alias_row_inline_open")
}

func TestRoot_FirstRunRunOnChooser_IncludesChatGPT(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus: "ready",
		DaemonState:  "up",
	})
	viewport := geom.Rect{W: 100, H: 30}
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "routing")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "provider")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	out := rt.Render(viewport).String()
	if !strings.Contains(out, "ChatGPT") {
		t.Fatalf("run-on provider chooser missing ChatGPT option: %q", out)
	}
}

func TestRoot_FirstRunChatGPTBrowserLogin_ShowsAuthFlowRows(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		CreateDraftName: "acme",
		CreateDraftProviderConfig: state.ProviderConfigSnapshot{
			Ref:           state.DraftProviderRef,
			ProviderSpec:  "chatgpt",
			CredentialRef: "chatgpt_login",
		},
	})
	viewport := geom.Rect{W: 100, H: 30}
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "routing")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	out := rt.Render(viewport).String()
	if !(strings.Contains(out, "sign in") && strings.Contains(out, "open default browser")) &&
		!strings.Contains(strings.ToLower(out), "chatgpt · browser login") {
		t.Fatalf("first-run browser login rows missing: %q", out)
	}
	if strings.Contains(out, "use device code") {
		t.Fatalf("first-run browser login should not show fallback before browser-start failure: %q", out)
	}
}

func TestRoot_FirstRunChatGPTBrowserLogin_LongURLVisualRoundTrip_NoEllipsisNoLoss(t *testing.T) {
	t.Parallel()

	longURL := "https://auth.openai.com/oauth/authorize?client_id=app_EMoamEEZ73f0CkXaXp7hrann&code_challenge=D2PFtaHyPdOn2qqnmR4HsI4r6cai8uWAYJgaJm4GdTw&code_challenge_method=S256&code_simplified_flow=true&id_token_add_organizations=true&originator=codex_cli_rs&redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback&response_type=code&scope=openid+email+offline_access+api.connectors.read+api.connectors.invoke&state=qBBX8qSY-WyEkOMpHdQCJkj4A"
	rt := newTestRuntime(state.Model{
		HeaderStatus: "ready",
		DaemonState:  "up",
		CreateDraftProviderConfig: state.ProviderConfigSnapshot{
			Ref:           "create-draft",
			ProviderSpec:  "chatgpt",
			CredentialRef: "chatgpt_login",
		},
		AuthSessions: map[string]stateModel.AuthSessionViewState{
			stateModel.CreateDraftAuthOwnerKey("create-draft").String(): {
				SessionID:    "sess-1",
				URL:          longURL,
				SessionState: "pending",
			},
		},
	})
	viewport := geom.Rect{W: 100, H: 30}
	rt.Rebuild(Root(), viewport)
	openRoutingSection(t, rt, viewport)
	out := rt.Render(viewport).String()
	if strings.Contains(out, "…") {
		t.Fatalf("expected no ellipsis in wrapped auth link rows; render=%q", out)
	}
	normalizedRender := normalizeVisualText(out)
	normalizedURL := normalizeVisualText(longURL)
	if !strings.Contains(normalizedRender, normalizedURL) {
		t.Fatalf("rendered auth url lost characters across wraps; render=%q", out)
	}
}

func TestRoot_FirstRunChatGPTBrowserLogin_LongURLNarrowViewport_NoLoss(t *testing.T) {
	t.Parallel()

	longURL := "https://auth.openai.com/oauth/authorize?client_id=app_EMoamEEZ73f0CkXaXp7hrann&code_challenge=D2PFtaHyPdOn2qqnmR4HsI4r6cai8uWAYJgaJm4GdTw&code_challenge_method=S256&code_simplified_flow=true&id_token_add_organizations=true&originator=codex_cli_rs&redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback&response_type=code&scope=openid+email+offline_access+api.connectors.read+api.connectors.invoke&state=qBBX8qSY-WyEkOMpHdQCJkj4A"
	rt := newTestRuntime(state.Model{
		HeaderStatus: "ready",
		DaemonState:  "up",
		CreateDraftProviderConfig: state.ProviderConfigSnapshot{
			Ref:           "create-draft",
			ProviderSpec:  "chatgpt",
			CredentialRef: "chatgpt_login",
		},
		AuthSessions: map[string]stateModel.AuthSessionViewState{
			stateModel.CreateDraftAuthOwnerKey("create-draft").String(): {
				SessionID:    "sess-1",
				URL:          longURL,
				SessionState: "pending",
			},
		},
	})
	viewport := geom.Rect{W: 80, H: 24}
	rt.Rebuild(Root(), viewport)
	openRoutingSection(t, rt, viewport)
	out := rt.Render(viewport).String()
	normalizedRender := normalizeVisualText(out)
	if !strings.Contains(normalizedRender, "code_challenge_method=S256") {
		t.Fatalf("expected code_challenge_method token in narrow viewport render; render=%q", out)
	}
	if !strings.Contains(normalizedRender, "redirect_uri=http%3A%2F%2Flocalhost%3A1455%2Fauth%2Fcallback") {
		t.Fatalf("expected redirect_uri token in narrow viewport render; render=%q", out)
	}
}

func TestRoot_FirstRunChatGPTSignedIn_HidesKeychainEditRows(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus: "ready",
		DaemonState:  "up",
		CreateDraftProviderConfig: state.ProviderConfigSnapshot{
			ProviderSpec:  "chatgpt",
			CredentialRef: "keychain:chatgpt/sess_abc",
		},
	})
	viewport := geom.Rect{W: 100, H: 30}
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "routing")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	out := rt.Render(viewport).String()
	if !strings.Contains(out, "signed in") {
		t.Fatalf("expected signed-in auth summary in first run: %q", out)
	}
	if strings.Contains(out, "key slot") || strings.Contains(out, "key value") {
		t.Fatalf("unexpected keychain edit rows for chatgpt signed-in first run: %q", out)
	}
}

func normalizeVisualText(value string) string {
	return strings.Join(strings.Fields(value), "")
}

func TestRoot_ChatGPTAddModelAuthFlowVisualGrammar(t *testing.T) {
	t.Parallel()

	base := state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
		EndpointSnapshots: []state.EndpointSnapshot{
			{
				Name:                      "acme",
				SelectedProviderConfigRef: "openai:gpt-3.5-turbo",
				ProviderConfigs: []state.ProviderConfigSnapshot{
					{
						Ref:              "openai:gpt-3.5-turbo",
						ProviderSpec:     "openai",
						ModelID:          "gpt-3.5-turbo",
						ProviderProtocol: "auto",
						CredentialRef:    "env:OPENAI_API_KEY",
					},
				},
			},
		},
	}

	viewport := geom.Rect{W: 100, H: 30}

	t.Run("browser_not_started", func(t *testing.T) {
		rt := newTestRuntime(base)
		rt.Rebuild(Root(), viewport)
		openAddModelAndChooseProvider(t, rt, viewport, "ChatGPT")
		out := rt.Render(viewport).String()
		assertRootScenario(t, out, "browser_not_started")
	})

	t.Run("device_in_progress", func(t *testing.T) {
		rt := newTestRuntime(base)
		rt.Rebuild(Root(), viewport)
		openAddModelAndChooseProvider(t, rt, viewport, "ChatGPT")
		rt.Dispatch([]update.Action{
			stateeffect.ProviderAuthSessionStarted{
				EndpointName: "acme",
				ProviderConfig: state.ProviderConfigSnapshot{
					Ref:           "model-2",
					ProviderSpec:  "chatgpt",
					BaseURL:       "https://api.openai.com/v1",
					CredentialRef: "chatgpt_device_auth",
				},
				AuthScope:    stateModel.AuthScopeEndpointProvider,
				OwnerKey:     stateModel.AddModelDraftAuthOwnerKey("acme", "model-2").String(),
				SessionID:    "sess-1",
				AuthorizeURL: "https://chatgpt.com/activate",
				UserCode:     "VBMS-V2R4K",
				State:        "pending",
			},
		})
		rt.Model.HeaderStatus = "ready"
		rt.Model.InteractionMode = state.InteractionModeManageList
		rt.Rebuild(Root(), viewport)
		out := rt.Render(viewport).String()
		assertRootScenario(t, out, "device_in_progress")
	})
	t.Run("signed_in", func(t *testing.T) {
		rt := newTestRuntime(base)
		rt.Rebuild(Root(), viewport)
		openAddModelAndChooseProvider(t, rt, viewport, "ChatGPT")
		rt.Dispatch([]update.Action{
			stateeffect.ProviderAuthSessionCredentialResolvedAction{
				EndpointName: "acme",
				ProviderConfig: state.ProviderConfigSnapshot{
					Ref:           "model-2",
					ProviderSpec:  "chatgpt",
					BaseURL:       "https://api.openai.com/v1",
					CredentialRef: "chatgpt_login",
				},
				AuthScope:     stateModel.AuthScopeEndpointProvider,
				OwnerKey:      stateModel.AddModelDraftAuthOwnerKey("acme", "model-2").String(),
				CredentialRef: "keychain:chatgpt/default",
			},
		})
		rt.Rebuild(Root(), viewport)
		out := rt.Render(viewport).String()
		assertRootScenario(t, out, "signed_in")
	})
}

func TestRoot_EscOnExpandedRoutingModelClosesNearestModelDisclosure(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
		EndpointSnapshots: []state.EndpointSnapshot{
			{
				Name:                      "acme",
				SelectedProviderConfigRef: "openai:gpt-5.3",
				ProviderConfigs: []state.ProviderConfigSnapshot{
					{
						Ref:              "openai:gpt-5.3",
						ProviderSpec:     "openai",
						ModelID:          "gpt-5.3",
						ProviderProtocol: "auto",
						CredentialRef:    "env:OPENAI_API_KEY",
					},
					{
						Ref:              "anthropic:opus",
						ProviderSpec:     "anthropic",
						ModelID:          "opus",
						ProviderProtocol: "auto",
						CredentialRef:    "env:ANTHROPIC_API_KEY",
					},
				},
			},
		},
	})
	viewport := geom.Rect{W: 100, H: 30}
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "routing")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "models")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	out := rt.Render(viewport).String()
	assertRootScenario(t, out, "models_disclosure_open")

	focusRowContaining(t, rt, viewport, "models")
	rt.DispatchEvent(updateKey(interaction.KeyEsc))
	rt.Rebuild(Root(), viewport)

	out = rt.Render(viewport).String()
	assertRootScenario(t, out, "models_disclosure_closed_by_esc")
}

func TestRoot_FirstRunOpeningModelClosesCredentialChooserDisclosure(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		CreateDraftName: "acme",
		CreateDraftProviderConfig: state.ProviderConfigSnapshot{
			Ref:           state.DraftProviderRef,
			ProviderSpec:  "bedrock",
			BaseURL:       "https://bedrock-runtime.us-east-1.amazonaws.com/openai/v1",
			CredentialRef: "profile:default",
		},
		CreateDraftModelIDs:           []string{"amazon.nova-pro-v1:0"},
		CreateDraftModelProviderSpec:  "bedrock",
		CreateDraftModelBaseURL:       "https://bedrock-runtime.us-east-1.amazonaws.com/openai/v1",
		CreateDraftModelCredentialRef: "profile:default",
	})
	viewport := geom.Rect{W: 110, H: 34}
	rt.Rebuild(Root(), viewport)
	openRoutingSection(t, rt, viewport)
	focusRowContaining(t, rt, viewport, "credential")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
	if out := rt.Render(viewport).String(); !strings.Contains(out, "Bedrock API key") {
		t.Fatalf("expected credential chooser open with Bedrock options; render=%q", out)
	}

	rt.DispatchEvent(updateKey(interaction.KeyEsc))
	rt.Rebuild(Root(), viewport)
	focusRowContaining(t, rt, viewport, "model")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	out := rt.Render(viewport).String()
	if strings.Contains(out, "Bedrock API key") {
		t.Fatalf("credential chooser should close when model chooser opens; render=%q", out)
	}
	if !strings.Contains(out, "loading models") {
		t.Fatalf("expected model load state after opening model row; render=%q", out)
	}
}

func TestRoot_WorkspaceSavedStatusDoesNotRenderCopyEndpointHintRows(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "saved",
		DaemonState:     "up",
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
	})
	viewport := geom.Rect{W: 80, H: 24}
	rt.Rebuild(Root(), viewport)
	out := rt.Render(viewport).String()
	if strings.Contains(out, "copy endpoint") {
		t.Fatalf("render contains unexpected copy hint row: %q", out)
	}
}

func TestRoot_ControlPlaneIncompatibleRendersHardStopMismatchScreen(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus: "incompatible",
		ControlPlane: &state.ControlPlaneMismatch{
			ExpectedProtocol:  7,
			DaemonProtocol:    6,
			HasDaemonProtocol: true,
			TUIVersion:        "0.9.0",
			DaemonVersion:     "0.8.4",
			RecoveryCommand:   "swobu daemon restart",
		},
	})
	viewport := geom.Rect{W: 80, H: 24}
	rt.Rebuild(Root(), viewport)
	out := rt.Render(viewport).String()
	assertContainsInOrder(t, out,
		"incompatible   [ daemon mismatch ]",
		"mismatch",
		"recover",
		"↑↓ move",
	)
	if strings.Contains(out, "workspace") || strings.Contains(out, "traffic") {
		t.Fatalf("hard-stop mismatch screen should hide normal sections: %q", out)
	}
}
