package ports

import (
	"context"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

// RunExecutor executes a disclosed run command after the run-once feature owns
// confirmation.
type RunExecutor interface {
	ExecuteRunCommand(ctx context.Context, request ExecuteRunCommandRequest) (RunExecutionResult, error)
}

// ExecuteRunCommandRequest selects the disclosed command and optional target.
type ExecuteRunCommandRequest struct {
	WorkspaceID  readmodel.WorkspaceID
	RunCommandID readmodel.RunCommandID
	RouteID      readmodel.RouteID
}

// RunExecutionResult reports the activity row produced by a run command when
// the adapter can observe one.
type RunExecutionResult struct {
	ActivityID readmodel.ActivityID
}
