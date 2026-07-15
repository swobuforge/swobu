package cockpit

import (
	"context"
	"time"

	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

// ModelReloader owns the refresh policy after workspace mutations. It loads
// fresh cockpit state from the daemon, reconciles it with the mutation that
// triggered the refresh, and produces a notice when the load is stale.
//
// The component layer (Cockpit) renders and routes events; the reloader owns
// the policy of what to load, when to fall back, and what notice to show.
type ModelReloader struct {
	ctx     context.Context
	query   ports.WorkspaceQueries
	timeout time.Duration
}

// NewModelReloader builds a reloader with the given query surface and context.
// A nil query means refresh is disabled; the reloader falls back to local
// model patching immediately.
func NewModelReloader(ctx context.Context, query ports.WorkspaceQueries, timeout time.Duration) *ModelReloader {
	if ctx == nil {
		ctx = context.Background()
	}
	return &ModelReloader{ctx: ctx, query: query, timeout: timeout}
}

// RefreshAfterSave loads a fresh cockpit projection after a workspace save.
// It returns the reconciled model and a notice. When the daemon is
// unreachable, it patches the current model in place and reports a stale
// notice so the UI does not silently hide the mutation.
func (r *ModelReloader) RefreshAfterSave(current readmodel.CockpitReadModel, saved readmodel.WorkspaceReadModel) (readmodel.CockpitReadModel, readmodel.Notice) {
	if r.query == nil {
		return selectWorkspace(updateWorkspaceInModel(current, saved), saved.ID), readmodel.Notice{}
	}
	ctx, cancel := r.refreshContext()
	defer cancel()
	fresh, err := r.query.LoadCockpit(ctx)
	if err != nil {
		return selectWorkspace(updateWorkspaceInModel(current, saved), saved.ID),
			staleRefreshNotice("refresh stale: saved workspace shown; " + err.Error())
	}
	workspace, workspaceErr := r.query.LoadWorkspace(ctx, saved.ID)
	if workspaceErr != nil {
		workspace = saved
		return selectWorkspace(updateWorkspaceInModel(fresh, workspace), workspace.ID),
			staleRefreshNotice("refresh stale: saved workspace shown; " + workspaceErr.Error())
	}
	return selectWorkspace(updateWorkspaceInModel(fresh, workspace), workspace.ID), readmodel.Notice{}
}

// RefreshAfterDelete loads a fresh cockpit projection after a workspace
// deletion. It returns the reconciled model and a notice. When the daemon
// is unreachable, it patches the current model in place.
func (r *ModelReloader) RefreshAfterDelete(current readmodel.CockpitReadModel, deleted readmodel.WorkspaceID) (readmodel.CockpitReadModel, readmodel.Notice) {
	if r.query == nil {
		return removeWorkspaceFromModel(current, deleted), readmodel.Notice{}
	}
	ctx, cancel := r.refreshContext()
	defer cancel()
	fresh, err := r.query.LoadCockpit(ctx)
	if err != nil {
		return removeWorkspaceFromModel(current, deleted),
			staleRefreshNotice("refresh stale: deleted workspace hidden; " + err.Error())
	}
	return removeWorkspaceFromModel(fresh, deleted), readmodel.Notice{}
}

func (r *ModelReloader) refreshContext() (context.Context, context.CancelFunc) {
	base := r.ctx
	if base == nil {
		base = context.Background()
	}
	return context.WithTimeout(base, r.timeout)
}
