package cockpit

import (
	"context"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

// ModelReloader owns the refresh policy after workspace mutations. It loads
// fresh cockpit state from the daemon and reports reconciliation facts without
// choosing their presentation scope.
//
// The component layer (Cockpit) renders and routes events; the reloader owns
// the policy of what to load, when to fall back, and what notice to show.
type ModelReloader struct {
	ctx     context.Context
	query   ports.WorkspaceQueries
	timeout time.Duration
}

// Reconciliation reports the resulting projection and whether the root
// Cockpit model was freshly loaded. A later workspace-detail failure remains
// local to that workspace and does not make the root projection stale.
type Reconciliation struct {
	Model     readmodel.CockpitReadModel
	Err       error
	RootFresh bool
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

// RefreshAfterSave reconciles a workspace projection after a rename or the
// authoritative first-target response. Local draft naming also uses the local
// projection path but never queries or mutates the daemon. When the daemon is
// unreachable for persisted renames, it patches the current model and reports
// the refresh error without claiming authoritative reconciliation.
func (r *ModelReloader) RefreshAfterSave(current readmodel.CockpitReadModel, saved readmodel.WorkspaceReadModel) Reconciliation {
	if r.query == nil || current.SelectedWorkspace.IsOnboarding() {
		return Reconciliation{Model: r.localSaveProjection(current, saved)}
	}
	ctx, cancel := r.refreshContext()
	defer cancel()
	fresh, err := r.query.LoadCockpit(ctx)
	if err != nil {
		return Reconciliation{Model: r.localSaveProjection(current, saved), Err: err}
	}
	workspace, workspaceErr := r.query.LoadWorkspace(ctx, saved.ID)
	if workspaceErr != nil {
		workspace = mergeWorkspaceProjection(current.SelectedWorkspace, saved)
		return Reconciliation{Model: selectWorkspace(updateWorkspaceInModel(fresh, workspace), workspace.ID), Err: workspaceErr, RootFresh: true}
	}
	return Reconciliation{Model: selectWorkspace(updateWorkspaceInModel(fresh, workspace), workspace.ID), RootFresh: true}
}

// RefreshAfterDelete loads a fresh cockpit projection after a workspace
// deletion. When the daemon is unreachable, it patches the current model in
// place without claiming authoritative reconciliation.
func (r *ModelReloader) RefreshAfterDelete(current readmodel.CockpitReadModel, deleted readmodel.WorkspaceID) Reconciliation {
	if r.query == nil {
		return Reconciliation{Model: removeWorkspaceFromModel(current, deleted)}
	}
	ctx, cancel := r.refreshContext()
	defer cancel()
	fresh, err := r.query.LoadCockpit(ctx)
	if err != nil {
		return Reconciliation{Model: removeWorkspaceFromModel(current, deleted), Err: err}
	}
	return Reconciliation{Model: removeWorkspaceFromModel(fresh, deleted), RootFresh: true}
}

func (r *ModelReloader) refreshContext() (context.Context, context.CancelFunc) {
	base := r.ctx
	if base == nil {
		base = context.Background()
	}
	return context.WithTimeout(base, r.timeout)
}

// localSaveProjection applies a command result without querying the daemon.
// Naming retains the [+] draft identity; the first-target result carries an
// authoritative persisted workspace and promotes it to a normal tab.
func (r *ModelReloader) localSaveProjection(current readmodel.CockpitReadModel, saved readmodel.WorkspaceReadModel) readmodel.CockpitReadModel {
	oldID := current.SelectedWorkspaceID
	merged := mergeWorkspaceProjection(current.SelectedWorkspace, saved)
	if !merged.IsOnboarding() && merged.WorkspaceURL == "" {
		merged.WorkspaceURL = derivedWorkspaceURL(current, merged.Slug)
	}
	return selectWorkspace(replaceWorkspaceIdentity(current, oldID, merged), saved.ID)
}

func derivedWorkspaceURL(current readmodel.CockpitReadModel, slug string) string {
	slug = strings.TrimSpace(slug) // swobu:io-string source=boundary
	if slug == "" {
		return ""
	}
	for _, candidate := range []string{
		strings.TrimSpace(current.SelectedWorkspace.WorkspaceURL), // swobu:io-string source=boundary
		strings.TrimSpace(current.HeaderRight),                    // swobu:io-string source=boundary
	} {
		if candidate == "" || !strings.Contains(candidate, "://") {
			continue
		}
		if i := strings.LastIndex(candidate, "/c/"); i >= 0 {
			return candidate[:i+len("/c/")] + slug
		}
		return strings.TrimRight(candidate, "/") + "/c/" + slug
	}
	return "http://127.0.0.1:7926/c/" + slug
}

func mergeWorkspaceProjection(current readmodel.WorkspaceReadModel, saved readmodel.WorkspaceReadModel) readmodel.WorkspaceReadModel {
	merged := current
	if saved.ID != "" {
		merged.ID = saved.ID
	}
	if saved.Slug != "" {
		merged.Slug = saved.Slug
	}
	merged.State = saved.State
	if saved.WorkspaceURL != "" {
		merged.WorkspaceURL = saved.WorkspaceURL
	}
	if len(merged.Routes) == 0 && len(saved.Routes) > 0 {
		merged.Routes = saved.Routes
	}
	if merged.Activity.IsEmpty() && !saved.Activity.IsEmpty() {
		merged.Activity = saved.Activity
	}
	return merged
}
