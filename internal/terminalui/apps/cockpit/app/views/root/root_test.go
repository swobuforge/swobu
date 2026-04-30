package root

import (
	"fmt"
	"strings"
	"testing"

	"github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/loop"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/rendergraph/geom"
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
		"Swobu",
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
	if len(effects) != 5 {
		t.Fatalf("on-mount effect count = %d, want 5", len(effects))
	}
	if _, ok := effects[4].(state.ScheduleDaemonRefreshEffect); !ok {
		t.Fatalf("on-mount effect[4] = %T, want state.ScheduleDaemonRefreshEffect", effects[4])
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

func TestRoot_EscOnOpenRoutingSectionCollapsesSectionBeforeExit(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		CreateDraftName: "acme",
		CreateDraftProviderConfig: state.ProviderConfigSnapshot{
			Ref:           state.DraftProviderRef,
			ProviderSpec:  "openai",
			ProtocolKind:  "chat_completions",
			CredentialRef: "env:OPENAI_API_KEY",
			ModelID:       "gpt-5.3",
		},
	})
	viewport := geom.Rect{W: 80, H: 24}
	rt.Rebuild(Root(), viewport)
	focusRowContaining(t, rt, viewport, "run on")

	rt.DispatchEvent(updateKey(interaction.KeyEsc))
	rt.Rebuild(Root(), viewport)

	out := rt.Render(viewport).String()
	if !strings.Contains(out, "routing ▸") {
		t.Fatalf("expected routing section to collapse on esc; render=%q", out)
	}
	if strings.Contains(out, "run on") {
		t.Fatalf("expected routing child rows hidden after esc collapse; render=%q", out)
	}
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
	if !strings.Contains(out, `model_provider = "swobu"`) || !strings.Contains(out, `base_url = "http://127.0.0.1:7777/c/acme/v1"`) {
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
	if strings.Contains(out, `"baseURL": "http://127.0.0.1:7777/c/acme/v1"`) {
		t.Fatalf("expected OpenCode baseURL to be below initial disclosure viewport before scrolling; render=%q", out)
	}
	for i := 0; i < 8; i++ {
		rt.DispatchEvent(updateKey(interaction.KeyDown))
		rt.Rebuild(Root(), viewport)
	}
	out = rt.Render(viewport).String()
	if !strings.Contains(out, `"baseURL":`) || !strings.Contains(out, `"http://127.0.0.1:7777/c/acme/v1"`) {
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
	if !strings.Contains(out, "Swobu! 🧌") {
		t.Fatalf("expected header rail visible in compact viewport during long payload disclosure; render=%q", out)
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
	if strings.Contains(out, `model_providers.swobu.base_url="http://127.0.0.1:7777/c/acme/v1"`) {
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
						Ref:           "ollama:gemma4",
						ProviderSpec:  "ollama",
						ModelID:       "gemma4:e4b",
						ProtocolKind:  "chat_completions",
						CredentialRef: "",
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
						Ref:           "openai:gpt-5.3",
						ProviderSpec:  "openai",
						ModelID:       "gpt-5.3",
						ProtocolKind:  "chat_completions",
						CredentialRef: "env:OPENAI_API_KEY",
					},
					{
						Ref:           "anthropic:opus",
						ProviderSpec:  "anthropic",
						ModelID:       "opus",
						ProtocolKind:  "chat_completions",
						CredentialRef: "env:ANTHROPIC_API_KEY",
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
	focusRowContaining(t, rt, viewport, "run on")

	focusRowContaining(t, rt, viewport, "models")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	out := rt.Render(viewport).String()
	if !strings.Contains(out, "gpt-5.3") || !strings.Contains(out, "opus") {
		t.Fatalf("expected concise model rows in models drawer; render=%q", out)
	}
	if strings.Contains(out, "provider:") || strings.Contains(out, "selected") {
		t.Fatalf("unexpected legacy summary clutter in model rows; render=%q", out)
	}

	focusRowContaining(t, rt, viewport, "add model")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	out = rt.Render(viewport).String()
	if !strings.Contains(out, "model             not set") {
		t.Fatalf("expected add-model model row in create lane; render=%q", out)
	}
	if strings.Contains(out, "primary            yes") {
		t.Fatalf("unexpected static primary row in add-model create lane; render=%q", out)
	}
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
				SelectedProviderConfigRef: "openai:gpt-5.3",
				ProviderConfigs: []state.ProviderConfigSnapshot{
					{
						Ref:           "openai:gpt-5.3",
						ProviderSpec:  "openai",
						ModelID:       "gpt-5.3",
						ProtocolKind:  "chat_completions",
						CredentialRef: "env:OPENAI_API_KEY",
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

	focusRowContaining(t, rt, viewport, "add model")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	out := rt.Render(viewport).String()
	if !strings.Contains(out, "id                not set") || !strings.Contains(out, "edit ↵") {
		t.Fatalf("expected id row edit affordance in models disclosure; render=%q", out)
	}
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
						Ref:           "openai:gpt-5.3",
						ProviderSpec:  "openai",
						ModelID:       "gpt-5.3",
						ProtocolKind:  "chat_completions",
						CredentialRef: "env:OPENAI_API_KEY",
					},
					{
						Ref:           "anthropic:opus",
						ProviderSpec:  "anthropic",
						ModelID:       "opus",
						ProtocolKind:  "chat_completions",
						CredentialRef: "env:ANTHROPIC_API_KEY",
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
	if !strings.Contains(out, "opus") {
		t.Fatalf("expected models disclosure rows visible before esc; render=%q", out)
	}

	focusRowContaining(t, rt, viewport, "models")
	rt.DispatchEvent(updateKey(interaction.KeyEsc))
	rt.Rebuild(Root(), viewport)

	out = rt.Render(viewport).String()
	if strings.Contains(out, "opus") {
		t.Fatalf("expected esc to close models disclosure rows; render=%q", out)
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

func TestRoot_ControlPlaneIncompatibleRendersHardStopCompatibilityScreen(t *testing.T) {
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
		"compatibility",
		"recover",
		"↑↓ move",
	)
	if strings.Contains(out, "workspace") || strings.Contains(out, "traffic") {
		t.Fatalf("hard-stop compatibility screen should hide normal sections: %q", out)
	}
}

func TestRoot_TrafficRowsRemainNavigableAfterOpeningRow(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
		TrafficRows: []state.TrafficRow{
			{RequestID: "req-3", OperationFamily: "responses", Target: "backend-a", Result: "in_progress", StatusCode: 0, ObservedAt: "11:11:03"},
			{RequestID: "req-2", OperationFamily: "responses", Target: "backend-a", Result: "in_progress", StatusCode: 0, ObservedAt: "11:11:02"},
			{RequestID: "req-1", OperationFamily: "responses", Target: "backend-a", Result: "in_progress", StatusCode: 0, ObservedAt: "11:11:01"},
		},
	})
	viewport := geom.Rect{W: 120, H: 40}
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "traffic")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "11:11:03")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	rt.DispatchEvent(updateKey(interaction.KeyDown))
	rt.Rebuild(Root(), viewport)
	focusRowContaining(t, rt, viewport, "11:11:02")
}

func TestRoot_TrafficRows_WindowedListPreventsOverflow(t *testing.T) {
	t.Parallel()

	var trafficRows []state.TrafficRow
	for i := 1; i <= 12; i++ {
		id := fmt.Sprintf("req-%02d", i)
		when := fmt.Sprintf("11:22:%02d", i)
		trafficRows = append(trafficRows, state.TrafficRow{
			RequestID:       id,
			OperationFamily: "responses",
			Target:          "backend-a",
			Result:          "in_progress",
			StatusCode:      0,
			ObservedAt:      when,
		})
	}

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
		TrafficRows:     trafficRows,
	})
	viewport := geom.Rect{W: 120, H: 40}
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "traffic")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	out := rt.Render(viewport).String()
	const expectedWindow = 5
	if got := strings.Count(out, "11:22:"); got != expectedWindow {
		t.Fatalf("visible traffic rows=%d want %d; render=%q", got, expectedWindow, out)
	}
}

func TestRoot_TrafficRows_DownAtWindowEdgeScrollsWithinTrafficList(t *testing.T) {
	t.Parallel()

	var trafficRows []state.TrafficRow
	for i := 1; i <= 8; i++ {
		id := fmt.Sprintf("req-%02d", i)
		when := fmt.Sprintf("12:34:%02d", i)
		trafficRows = append(trafficRows, state.TrafficRow{
			RequestID:       id,
			OperationFamily: "responses",
			Target:          "backend-a",
			Result:          "in_progress",
			StatusCode:      0,
			ObservedAt:      when,
		})
	}

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
		TrafficRows:     trafficRows,
	})
	viewport := geom.Rect{W: 120, H: 40}
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "traffic")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "12:34:08")
	const windowRows = 5
	for i := 0; i < windowRows-1; i++ {
		rt.DispatchEvent(updateKey(interaction.KeyDown))
		rt.Rebuild(Root(), viewport)
	}
	focusRowContaining(t, rt, viewport, "12:34:04")

	rt.DispatchEvent(updateKey(interaction.KeyDown))
	rt.Rebuild(Root(), viewport)
	focusRowContaining(t, rt, viewport, "12:34:03")
}

func TestRoot_TrafficEmptyOpenRendersSummaryLineInsteadOfKVPadding(t *testing.T) {
	t.Parallel()

	rt := newTestRuntime(state.Model{
		HeaderStatus:    "ready",
		DaemonState:     "up",
		Endpoints:       []string{"acme"},
		CurrentEndpoint: "acme",
	})
	viewport := geom.Rect{W: 80, H: 24}
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "traffic")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	out := rt.Render(viewport).String()
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "no traffic yet") {
			continue
		}
		leadingSpaces := len(line) - len(strings.TrimLeft(line, " "))
		if leadingSpaces > 6 {
			t.Fatalf("traffic empty line has key/value style padding, want summary indent: %q", line)
		}
		return
	}
	t.Fatalf("render missing no-traffic line: %q", out)
}

func TestRoot_FirstRunRoutingCredentialChooserMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		provider      string
		baseURL       string
		credentialRef string
		summary       string
		chooser       bool
	}{
		{
			name:     "openrouter requires chooser",
			provider: "openrouter",
			baseURL:  "https://openrouter.ai/api/v1",
			summary:  "choose a key source",
			chooser:  true,
		},
		{
			name:     "ollama local hides chooser",
			provider: "ollama",
			baseURL:  "http://127.0.0.1:11434/v1",
			summary:  "not required",
			chooser:  false,
		},
		{
			name:     "custom remote requires chooser",
			provider: "custom",
			baseURL:  "https://api.example.com/v1",
			summary:  "choose a key source",
			chooser:  true,
		},
		{
			name:     "custom local hides chooser",
			provider: "custom",
			baseURL:  "http://localhost:11434/v1",
			summary:  "not required",
			chooser:  false,
		},
		{
			name:          "existing credential keeps chooser visible",
			provider:      "ollama",
			baseURL:       "http://127.0.0.1:11434/v1",
			credentialRef: "env:OLLAMA_API_KEY",
			summary:       "env",
			chooser:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rt := newTestRuntime(state.Model{
				HeaderStatus:    "ready",
				DaemonState:     "up",
				CreateDraftName: "acme",
				CreateDraftProviderConfig: state.ProviderConfigSnapshot{
					Ref:           state.DraftProviderRef,
					ProviderSpec:  tt.provider,
					BaseURL:       tt.baseURL,
					CredentialRef: tt.credentialRef,
					ProtocolKind:  "chat_completions",
				},
			})
			viewport := geom.Rect{W: 80, H: 24}
			rt.Rebuild(Root(), viewport)
			out := rt.Render(viewport).String()
			assertUseKeyFromRow(t, out, tt.summary, tt.chooser)
		})
	}
}

func newTestRuntime(model state.Model) *loop.AppLoop[state.Model] {
	return loop.New(model, state.Reduce)
}

func updateKey(key interaction.Key) interaction.Event {
	return interaction.Event{Kind: interaction.EventKey, Key: key}
}

func assertContainsInOrder(t *testing.T, text string, patterns ...string) {
	t.Helper()
	offset := 0
	for _, pattern := range patterns {
		index := strings.Index(text[offset:], pattern)
		if index < 0 {
			t.Fatalf("render missing %q in order: %q", pattern, text)
		}
		offset += index + len(pattern)
	}
}

func assertCockpitVocabulary(t *testing.T, out string) {
	t.Helper()
	for _, forbidden := range []string{
		"selected target",
		"targets",
		"provider config",
		"credential source",
		"quick launch",
		"\nbehavior\n",
	} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("render contains forbidden cockpit label %q: %q", forbidden, out)
		}
	}
}

func focusRowContaining(t *testing.T, rt *loop.AppLoop[state.Model], viewport geom.Rect, pattern string) {
	t.Helper()
	for i := 0; i < 20; i++ {
		out := rt.Render(viewport).String()
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, ">") && strings.Contains(line, pattern) {
				return
			}
		}
		rt.DispatchEvent(updateKey(interaction.KeyDown))
	}
	t.Fatalf("could not focus row containing %q; render=%q", pattern, rt.Render(viewport).String())
}

func selectClientFromChooser(t *testing.T, rt *loop.AppLoop[state.Model], viewport geom.Rect, label string) {
	t.Helper()
	focusRowContaining(t, rt, viewport, "clients")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, "client            ")
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)

	focusRowContaining(t, rt, viewport, label)
	rt.DispatchEvent(updateKey(interaction.KeyEnter))
	rt.Rebuild(Root(), viewport)
}

func assertUseKeyFromRow(t *testing.T, out, summary string, chooser bool) {
	t.Helper()

	found := false
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "credentials") {
			continue
		}
		found = true
		if !strings.Contains(line, summary) {
			t.Fatalf("credentials row missing summary %q: %q", summary, line)
		}
		if chooser && !strings.Contains(line, "choose ↵") {
			t.Fatalf("credentials row should advertise chooser: %q", line)
		}
		if !chooser && strings.Contains(line, "choose ↵") {
			t.Fatalf("credentials row should hide chooser: %q", line)
		}
	}
	if !found {
		t.Fatalf("render missing credentials row: %q", out)
	}
}
