package routes

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/app/operator/shares"
	"github.com/swobuforge/swobu/internal/cockpit/mountedrender"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/sharestate"
	"github.com/swobuforge/swobu/internal/testkit/cockpittestkit"
)

type routeShareCommandsStub struct {
	result    shares.Result
	revealErr error
}

func (s routeShareCommandsStub) IssueShare(context.Context, string, sharestate.Expiry) (shares.Result, error) {
	return s.result, nil
}

func TestRouteShareOperationFailureStaysWithKnownRoute(t *testing.T) {
	route := readmodel.RouteReadModel{ID: "chat", ModelName: "chat", Share: &readmodel.ShareReadModel{Hostname: "d-example.share.swobu.com", Never: true}}
	model := readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting, Routes: []readmodel.RouteReadModel{route}}
	section := Section(model, nil)
	section.ShareCommands = routeShareCommandsStub{revealErr: errors.New("share reveal unavailable")}
	section.State.ExpandedRoute.Set(route.ID)
	section.copyShare(route)
	frame := testkit.RenderMountedTrimmed(t, section, 100, 20)
	if !strings.Contains(frame, "share reveal unavailable") || !strings.Contains(frame, "retry ↵") {
		t.Fatalf("route Share operation failure was not local:\n%s", frame)
	}
}
func (s routeShareCommandsStub) RevealShare(context.Context, string) (shares.Result, error) {
	return s.result, s.revealErr
}
func (s routeShareCommandsStub) RevokeShare(context.Context, string) error { return nil }

func TestRouteShareClipboardFailureStaysWithKnownRouteAndNeverPersistsSecret(t *testing.T) {
	route := readmodel.RouteReadModel{ID: "chat", ModelName: "chat", Share: &readmodel.ShareReadModel{Hostname: "d-example.share.swobu.com", Never: true}}
	model := readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting, Routes: []readmodel.RouteReadModel{route}}
	section := Section(model, nil)
	section.ShareCommands = routeShareCommandsStub{result: shares.Result{ShareURL: "https://d-example.share.swobu.com/#swsh_SECRET"}}
	section.State.ExpandedRoute.Set(route.ID)
	tempCalls := 0
	cleanup := ui.RegisterEffectHooks(nil, func(string) ui.ClipboardResult { return ui.ClipboardResult{Status: ui.CopyUnavailable} }, func(string, string, string) (string, error) { tempCalls++; return "/tmp/secret.txt", nil })
	defer cleanup()

	section.copyShare(route)
	frame := testkit.RenderMountedTrimmed(t, section, 100, 20)
	if tempCalls != 0 {
		t.Fatalf("route Share persisted secret %d times", tempCalls)
	}
	if !strings.Contains(frame, "clipboard unavailable") || !strings.Contains(frame, "retry ↵") {
		t.Fatalf("route Share failure was not local:\n%s", frame)
	}
	if got := section.State.ShareFeedback.Get(); got.Tone != ui.ToneWarning {
		t.Fatalf("unavailable tone = %v", got.Tone)
	}
}

func TestRouteShareCopyFailureUsesFailureTone(t *testing.T) {
	route := readmodel.RouteReadModel{ID: "chat", ModelName: "chat", Share: &readmodel.ShareReadModel{Hostname: "d-example.share.swobu.com", Never: true}}
	section := Section(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting, Routes: []readmodel.RouteReadModel{route}}, nil)
	section.ShareCommands = routeShareCommandsStub{result: shares.Result{ShareURL: "https://d-example.share.swobu.com/#swsh_SECRET"}}
	cleanup := ui.RegisterEffectHooks(nil, func(string) ui.ClipboardResult {
		return ui.ClipboardResult{Status: ui.CopyFailed, Err: errors.New("transport failed")}
	}, nil)
	defer cleanup()
	section.copyShare(route)
	if got := section.State.ShareFeedback.Get(); got.Message != "copy failed" || got.Tone != ui.ToneFailure {
		t.Fatalf("copy failure = %#v", got)
	}
}

func TestOnlyLatestRouteShareCopyCanExpireAcknowledgement(t *testing.T) {
	routeID := readmodel.RouteID("chat")
	section := Section(readmodel.WorkspaceReadModel{ID: "dev", Routes: []readmodel.RouteReadModel{{ID: routeID}}}, nil)
	section.State.ShareCopiedRoute.Set(routeID)
	section.shareCopyGeneration = 2

	section.clearShareCopied(routeID, 1)
	if got := section.State.ShareCopiedRoute.Get(); got != routeID {
		t.Fatalf("stale copy completion cleared latest acknowledgement: %q", got)
	}
	section.clearShareCopied(routeID, 2)
	if got := section.State.ShareCopiedRoute.Get(); got != "" {
		t.Fatalf("latest copy completion retained acknowledgement: %q", got)
	}
}

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
				t.Fatalf("width %d retained obsolete route vocabulary:\n%s", width, frame)
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
			screen := testkit.RenderMountedScreen(t, section, width, 20)
			frame := screen.String()
			if strings.Contains(frame, "target first") || strings.Contains(frame, "default           no") {
				t.Fatalf("zero-target route retained an inapplicable default row:\n%s", frame)
			}
			if !strings.Contains(frame, "add target") || !strings.Contains(frame, "add ↵") {
				t.Fatalf("zero-target route lost its next valid action:\n%s", frame)
			}
			testkit.AssertVisual("zero_target_expanded").
				Fixture(fmt.Sprintf("testdata/routes_section/fixture/zero_target_expanded_%d.ansi", width)).
				Viewport(width, 20).
				Now(t, screen)
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

func TestInlineTierTargetsShareValueColumns(t *testing.T) {
	model := readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting, Routes: []readmodel.RouteReadModel{{ID: "chat", ModelName: "chat", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{{ID: "a", Provider: "openai", Model: "gpt"}, {ID: "b", Provider: "anthropic", Model: "claude"}}}, {Targets: []readmodel.TargetReadModel{{ID: "c", Provider: "ollama", Model: "llama"}}}}}}}
	section := Section(model, nil)
	section.State.ExpandedRoute.Set("chat")
	screen := testkit.RenderMountedScreen(t, section, 100, 24)
	columns := map[string]int{}
	for _, line := range screen.Lines() {
		for _, value := range []string{"openai/gpt", "anthropic/claude", "ollama/llama"} {
			if at := strings.Index(line, value); at >= 0 {
				columns[value] = at
			}
		}
	}
	if columns["openai/gpt"] == 0 || columns["openai/gpt"] != columns["anthropic/claude"] || columns["openai/gpt"] != columns["ollama/llama"] {
		t.Fatalf("tier target value columns = %#v\n%s", columns, screen.String())
	}
}

func TestFirstTargetEditorKeepsOneInertTierContext(t *testing.T) {
	targets := []readmodel.TargetReadModel{{ID: "a", Provider: "openai", Model: "gpt"}, {ID: "b", Provider: "anthropic", Model: "claude"}}
	model := readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting, Routes: []readmodel.RouteReadModel{{ID: "chat", ModelName: "chat", Tiers: []readmodel.TierReadModel{{Targets: targets}}}}}
	section := Section(model, nil)
	section.State.ExpandedRoute.Set("chat")
	section.OpenTargetEditor(model.Routes[0], targets[0])
	frame := testkit.RenderMountedTrimmed(t, section, 100, 28)
	if strings.Count(frame, "primary") != 2 || !strings.Contains(frame, "edit target") || !strings.Contains(frame, "anthropic/claude") {
		t.Fatalf("first-target editor lost tier context or continuation:\n%s", frame)
	}
}

func TestTierMetadataAddsNoSelectionStops(t *testing.T) {
	model := readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting, Routes: []readmodel.RouteReadModel{{ID: "chat", ModelName: "chat", Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{{ID: "a", Provider: "openai", Model: "gpt"}, {ID: "b", Provider: "anthropic", Model: "claude"}}}, {Targets: []readmodel.TargetReadModel{{ID: "c", Provider: "ollama", Model: "llama"}}}}}}}
	section := Section(model, nil)
	section.State.ExpandedRoute.Set("chat")
	h, err := testkit.NewHarnessAt(section, 100, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	h.Open()
	for range 5 { // section disclosure, route, name, default, share
		h.FocusNext()
		h.Frame()
	}
	for _, want := range []string{"openai/gpt", "anthropic/claude", "ollama/llama", "add target"} {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
		frame := h.FrameTrimmed()
		if !selectedLineContains(frame, want) {
			t.Fatalf("selection did not move directly to %q:\n%s", want, frame)
		}
	}
}

func selectedLineContains(frame, value string) bool {
	for _, line := range strings.Split(frame, "\n") {
		if strings.Contains(line, ">") && strings.Contains(line, value) {
			return true
		}
	}
	return false
}
