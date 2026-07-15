package workspace_overview

import (
	"context"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestRunOnce_OpeningDoesNotExecuteImmediately(t *testing.T) {
	var called bool
	section := Section(runOnceSectionModel(), fakeWorkspaceCommands{
		run: func(context.Context, ports.ExecuteRunCommandRequest) (ports.RunExecutionResult, error) {
			called = true
			return ports.RunExecutionResult{}, nil
		},
	})

	section.openRun(section.Model.RunCommands[0])
	if called {
		t.Fatal("run executor should not fire on open")
	}
	if section.OpenRun.Get() != "codex" {
		t.Fatalf("open run = %q, want codex", section.OpenRun.Get())
	}
}

func TestRunOnce_DisclosureVisual(t *testing.T) {
	section := Section(runOnceSectionModel(), fakeWorkspaceCommands{})
	section.OpenRun.Set("codex")

	rendered := testkit.RenderTrimmed(section.Render(nil), 100, 14)
	testkit.AssertVisual("run_once_open").
		Fixture("testdata/run_once/fixture/open.txt").
		Viewport(100, 14).
		Now(t, rendered)
}

func TestRunOnce_ActivationShowsDisclosureInAppLoop(t *testing.T) {
	h := makeRunOnceHarness(t)
	defer h.Close()

	openRunOnceRow(t, h)

	frame := h.Frame()
	if !strings.Contains(frame, "model") {
		t.Fatalf("run-once disclosure frame missing model row:\n%s", frame)
	}
	if !strings.Contains(frame, "command") {
		t.Fatalf("run-once disclosure frame missing command row:\n%s", frame)
	}
}

func TestRunOnce_EscClosesDisclosureInAppLoop(t *testing.T) {
	h := makeRunOnceHarness(t)
	defer h.Close()

	openRunOnceRow(t, h)
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})

	frame := h.Frame()
	if strings.Contains(frame, "model") || strings.Contains(frame, "command") {
		t.Fatalf("run-once disclosure still visible after Esc:\n%s", frame)
	}
}

func runOnceSectionModel() readmodel.WorkspaceReadModel {
	return readmodel.WorkspaceReadModel{
		Slug:          "dev",
		State:         readmodel.WorkspaceExisting,
		ClientBaseURL: "http://127.0.0.1:7926/c/dev",
		RunCommands: []readmodel.RunCommandReadModel{{
			ID:             "codex",
			Label:          "Codex",
			CommandName:    "codex",
			TargetRouteID:  "gpt-4.1",
			TargetLabel:    "gpt-4.1",
			CommandPreview: "codex --model gpt-4.1",
		}},
		Routes: []readmodel.RouteReadModel{
			{ID: "gpt-4.1", ModelName: "gpt-4.1", Default: true},
			{ID: "gpt-4.1-alt", ModelName: "gpt-4.1-alt"},
		},
	}
}

func makeRunOnceHarness(t *testing.T) *testkit.MockAppHarness {
	t.Helper()
	h, err := testkit.NewHarness(&runOnceSurfaceRoot{SectionView: Section(runOnceSectionModel(), nil)})
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	h.Open()
	return h
}

func openRunOnceRow(t *testing.T, h *testkit.MockAppHarness) {
	t.Helper()
	h.FocusNext()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
}

type runOnceSurfaceRoot struct {
	*SectionView
}

func (r *runOnceSurfaceRoot) Render(app *tui.App) *tui.Element {
	return r.SectionView.Render(app)
}

func (r *runOnceSurfaceRoot) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnStop(tui.KeyUp, func(event tui.KeyEvent) {
			if app := event.App(); app != nil {
				app.FocusPrev()
			}
		}),
		tui.OnStop(tui.KeyDown, func(event tui.KeyEvent) {
			if app := event.App(); app != nil {
				app.FocusNext()
			}
		}),
		tui.OnStop(tui.KeyEnter, func(event tui.KeyEvent) {
			app := event.App()
			if app == nil || app.Focused() == nil {
				return
			}
			if element, ok := app.Focused().(*tui.Element); ok {
				element.Activate()
			}
		}),
		tui.OnStop(tui.KeyEscape, func(event tui.KeyEvent) {
			r.SectionView.Back()
		}),
	}
}

type fakeWorkspaceCommands struct {
	run func(context.Context, ports.ExecuteRunCommandRequest) (ports.RunExecutionResult, error)
}

func (f fakeWorkspaceCommands) SaveWorkspace(context.Context, ports.SaveWorkspaceRequest) (readmodel.WorkspaceReadModel, error) {
	return readmodel.WorkspaceReadModel{}, nil
}

func (f fakeWorkspaceCommands) DeleteWorkspace(context.Context, ports.DeleteWorkspaceRequest) error {
	return nil
}

func (f fakeWorkspaceCommands) ExecuteRunCommand(ctx context.Context, request ports.ExecuteRunCommandRequest) (ports.RunExecutionResult, error) {
	if f.run != nil {
		return f.run(ctx, request)
	}
	return ports.RunExecutionResult{}, nil
}
