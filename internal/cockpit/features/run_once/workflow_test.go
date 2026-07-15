package run_once

import (
	"context"
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestRunOnce_DisclosureShowsCommandPreview(t *testing.T) {
	workflow := NewWorkflow(runOnceWorkspace(), runOnceWorkspace().RunCommands[0], nil, nil)

	rendered := testkit.RenderTrimmed(workflow.Render(nil), 100, 8)
	testkit.AssertVisual("disclosure").
		Fixture("testdata/run_once_workflow/fixture/disclosure.txt").
		Viewport(100, 8).
		Now(t, rendered)
}

func TestRunOnce_ExecuteCallsPort(t *testing.T) {
	var got ports.ExecuteRunCommandRequest
	workflow := NewWorkflow(runOnceWorkspace(), runOnceWorkspace().RunCommands[0], func(_ context.Context, request ports.ExecuteRunCommandRequest) (ports.RunExecutionResult, error) {
		got = request
		return ports.RunExecutionResult{ActivityID: "req-1"}, nil
	}, nil)
	if got := workflow.Selected.Get(); got != "gpt-4.1" {
		t.Fatalf("selected route = %q, want gpt-4.1", got)
	}
	workflow.ChangeModel()
	if got := workflow.Selected.Get(); got != "gpt-4.1-alt" {
		t.Fatalf("selected route after change = %q, want gpt-4.1-alt", got)
	}

	workflow.Run(context.Background())

	if got.WorkspaceID != "dev" || got.RunCommandID != "codex" || got.RouteID != "gpt-4.1-alt" {
		t.Fatalf("execute request = %+v, want dev/codex/gpt-4.1-alt", got)
	}
	if workflow.Message.Get() != "started · req-1" {
		t.Fatalf("run message = %q, want started · req-1", workflow.Message.Get())
	}
}

func TestRunOnce_CancelClosesDetail(t *testing.T) {
	var closed bool
	workflow := NewWorkflow(runOnceWorkspace(), runOnceWorkspace().RunCommands[0], nil, func() {
		closed = true
	})

	if !workflow.Back() {
		t.Fatal("Back should consume the open workflow")
	}
	if !closed {
		t.Fatal("run-once close callback not invoked")
	}
}

func TestRunOnce_FailureShowsInlineError(t *testing.T) {
	workflow := NewWorkflow(runOnceWorkspace(), runOnceWorkspace().RunCommands[0], func(context.Context, ports.ExecuteRunCommandRequest) (ports.RunExecutionResult, error) {
		return ports.RunExecutionResult{}, errors.New("execution failed")
	}, nil)

	workflow.Run(context.Background())

	if got := workflow.Message.Get(); got != "execution failed" {
		t.Fatalf("message = %q, want execution failed", got)
	}
	if workflow.Phase.Get() != PhaseFailed {
		t.Fatalf("phase = %v, want failed", workflow.Phase.Get())
	}
}

func runOnceWorkspace() readmodel.WorkspaceReadModel {
	return readmodel.WorkspaceReadModel{
		ID:   "dev",
		Slug: "dev",
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
