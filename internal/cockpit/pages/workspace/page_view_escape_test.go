package workspace

import (
	"context"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/testkit/cockpittestkit"
)

func TestPage_EscapeBacksOutFromTargetToRouteSectionAndExit(t *testing.T) {
	target := readmodel.TargetReadModel{ID: "zai", Provider: string(profile.ProviderSpecZAI), Model: "glm-5", ZAIAccess: string(routing.ZAIAccessGeneralAPI), CredentialRef: "env:ZAI_API_KEY"}
	route := readmodel.RouteReadModel{ID: "chat", ModelName: "chat", Enabled: true, Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{target}}}}
	page := Page(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting, Routes: []readmodel.RouteReadModel{route}}, nil, nil, nil, context.Background(), nil, nil)
	h, err := testkit.NewHarness(page)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	h.Open()

	selectRow(t, h, "chat")
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	selectRow(t, h, "zai/glm-5")
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})
	if got := page.RoutesSection.State.OpenTarget.Get(); got != "" {
		t.Fatalf("target editor after first Escape = %q", got)
	}
	assertSelected(t, h, "zai/glm-5")
	assertRunning(t, h)

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})
	if got := page.RoutesSection.State.ExpandedRoute.Get(); got != "" {
		t.Fatalf("expanded route after second Escape = %q", got)
	}
	assertSelected(t, h, "chat")
	assertRunning(t, h)

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})
	if page.RoutesSection.Expanded.Get() {
		t.Fatal("routes section remained expanded after third Escape")
	}
	assertSelected(t, h, "model routes")
	assertRunning(t, h)

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})
	select {
	case <-h.App().StopCh():
	default:
		t.Fatal("final Escape did not stop Cockpit")
	}
}

func selectRow(t *testing.T, h *testkit.MockAppHarness, value string) {
	t.Helper()
	for range 64 {
		if strings.Contains(selectedLine(h.FrameTrimmed()), value) {
			return
		}
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	}
	t.Fatalf("did not select %q; selected line %q\n%s", value, selectedLine(h.FrameTrimmed()), h.FrameTrimmed())
}

func assertSelected(t *testing.T, h *testkit.MockAppHarness, value string) {
	t.Helper()
	if line := selectedLine(h.FrameTrimmed()); !strings.Contains(line, value) {
		t.Fatalf("selected line = %q, want %q\n%s", line, value, h.FrameTrimmed())
	}
}

func selectedLine(frame string) string {
	for _, line := range strings.Split(frame, "\n") {
		if strings.Contains(line, ">") {
			return line
		}
	}
	return ""
}

func assertRunning(t *testing.T, h *testkit.MockAppHarness) {
	t.Helper()
	select {
	case <-h.App().StopCh():
		t.Fatal("Cockpit stopped before semantic scopes were exhausted")
	default:
	}
}

func TestPage_EscapeClosesNestedTargetFieldBeforeTargetForm(t *testing.T) {
	route := readmodel.RouteReadModel{ID: "chat", ModelName: "chat", Enabled: true}
	page := Page(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting, Routes: []readmodel.RouteReadModel{route}}, nil, nil, nil, context.Background(), nil, nil)
	page.RoutesSection.State.ExpandedRoute.Set(route.ID)
	page.RoutesSection.AddTarget(route)
	config := page.RoutesSection.TargetConfigs.CachedAdd(route.ID)
	config.Draft.Set(readmodel.TargetDraft{ProviderSpec: string(profile.ProviderSpecZAI), CredentialRef: "env:ZAI_API_KEY"})
	config.SelectZAIAccess(string(routing.ZAIAccessGeneralAPI))

	h, err := testkit.NewHarness(page)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	h.Open()
	selectRow(t, h, "model")
	for _, r := range "discard-me" {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: r})
	}

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})
	if !config.IsOpen() || page.RoutesSection.State.AddTargetRoute.Get() != route.ID {
		t.Fatal("field Escape closed the target form")
	}
	if strings.Contains(h.FrameTrimmed(), "discard-me") {
		t.Fatalf("field Escape retained draft text:\n%s", h.FrameTrimmed())
	}

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})
	if page.RoutesSection.State.AddTargetRoute.Get() != "" {
		t.Fatalf("second Escape left target form open:\n%s", h.FrameTrimmed())
	}
	assertSelected(t, h, "add target")
	assertRunning(t, h)
}

func TestPage_EscapeDisarmsTargetDeleteBeforeClosingEditor(t *testing.T) {
	target := readmodel.TargetReadModel{ID: "zai", Provider: string(profile.ProviderSpecZAI), Model: "glm-5", ZAIAccess: string(routing.ZAIAccessGeneralAPI), CredentialRef: "env:ZAI_API_KEY"}
	route := readmodel.RouteReadModel{ID: "chat", ModelName: "chat", Enabled: true, Tiers: []readmodel.TierReadModel{{Targets: []readmodel.TargetReadModel{target}}}}
	page := Page(readmodel.WorkspaceReadModel{ID: "dev", Slug: "dev", State: readmodel.WorkspaceExisting, Routes: []readmodel.RouteReadModel{route}}, nil, nil, nil, context.Background(), nil, nil)
	h, err := testkit.NewHarness(page)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	h.Open()
	selectRow(t, h, "chat")
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	selectRow(t, h, "zai/glm-5")
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	selectRow(t, h, "delete            target")
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	config := page.RoutesSection.TargetConfigs.Edit(route, target)
	if !config.DeleteArmed.Get() {
		t.Fatal("target deletion did not arm")
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})
	if config.DeleteArmed.Get() {
		t.Fatal("Escape left target deletion armed")
	}
	if page.RoutesSection.State.OpenTarget.Get() != target.ID {
		t.Fatal("Escape closed editor while disarming target deletion")
	}
	assertRunning(t, h)
}
