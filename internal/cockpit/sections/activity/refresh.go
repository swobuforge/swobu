package activity

import (
	"context"
	"reflect"
	"sync"
	"time"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

type activityRefreshLifecycle struct {
	mu     sync.Mutex
	cancel context.CancelFunc
}

func (s *SectionView) stopActivityRefresh() {
	lifecycle := s.RefreshLifecycle
	if lifecycle == nil {
		return
	}
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	if lifecycle.cancel != nil {
		lifecycle.cancel()
		lifecycle.cancel = nil
	}
}

func (s *SectionView) startActivityRefresh(app *tui.App) {
	if app == nil || s.ActivityQuery == nil || s.Workspace.IsDraft() {
		return
	}
	query := s.ActivityQuery
	workspaceID := s.Workspace.ID
	snapshot := s.ActivitySnapshot
	lifecycle := s.RefreshLifecycle
	if lifecycle == nil {
		lifecycle = &activityRefreshLifecycle{}
		s.RefreshLifecycle = lifecycle
	}
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	if lifecycle.cancel != nil {
		lifecycle.cancel()
	}
	ctx, cancel := context.WithCancel(s.Ctx)
	lifecycle.cancel = cancel
	interval := s.RefreshInterval
	if interval <= 0 {
		interval = time.Second
	}
	go refreshActivityUntilStopped(ctx, app, interval, query, workspaceID, snapshot)
}

func refreshActivityUntilStopped(ctx context.Context, app *tui.App, interval time.Duration, query ports.ActivityQueries, workspaceID readmodel.WorkspaceID, snapshot *tui.State[readmodel.ActivityReadModel]) {
	refreshActivity(ctx, app, query, workspaceID, snapshot)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-app.StopCh():
			return
		case <-ticker.C:
			refreshActivity(ctx, app, query, workspaceID, snapshot)
		}
	}
}

func refreshActivity(ctx context.Context, app *tui.App, query ports.ActivityQueries, workspaceID readmodel.WorkspaceID, snapshot *tui.State[readmodel.ActivityReadModel]) {
	fresh, err := query.ListActivity(ctx, ports.ListActivityRequest{WorkspaceID: workspaceID, Limit: 5})
	if err != nil || ctx.Err() != nil || reflect.DeepEqual(fresh, snapshot.Get()) {
		return
	}
	app.QueueUpdate(func() {
		if !reflect.DeepEqual(fresh, snapshot.Get()) {
			snapshot.Set(fresh)
		}
	})
}

var _ tui.AppBinder = (*SectionView)(nil)
var _ tui.AppUnbinder = (*SectionView)(nil)
