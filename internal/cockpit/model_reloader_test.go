package cockpit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

func TestModelReloader_RefreshAfterSaveFallsBackOnLoadCockpitError(t *testing.T) {
	t.Parallel()
	fake := &fakeWorkspacePorts{loadCockpitErr: errors.New("daemon down")}
	reloader := NewModelReloader(context.Background(), fake, 5*time.Second)
	current := DefaultFixtureReadModel()

	model, notice := reloader.RefreshAfterSave(current, readmodel.WorkspaceReadModel{ID: "prod", Slug: "prod"})

	if model.SelectedWorkspaceID != "prod" {
		t.Fatalf("selected workspace = %q, want prod", model.SelectedWorkspaceID)
	}
	if notice.Kind != readmodel.NoticeStale {
		t.Fatalf("notice kind = %v, want NoticeStale", notice.Kind)
	}
	if fake.loadCockpitCalls != 1 {
		t.Fatalf("load cockpit calls = %d, want 1", fake.loadCockpitCalls)
	}
}

func TestModelReloader_RefreshAfterSaveFallsBackOnLoadWorkspaceError(t *testing.T) {
	t.Parallel()
	fake := &fakeWorkspacePorts{
		cockpit:          DefaultFixtureReadModel(),
		loadWorkspaceErr: errors.New("stale"),
	}
	reloader := NewModelReloader(context.Background(), fake, 5*time.Second)
	current := DefaultFixtureReadModel()

	model, notice := reloader.RefreshAfterSave(current, readmodel.WorkspaceReadModel{ID: "lab", Slug: "lab"})

	if model.SelectedWorkspaceID != "lab" {
		t.Fatalf("selected workspace = %q, want lab", model.SelectedWorkspaceID)
	}
	if notice.Kind != readmodel.NoticeStale {
		t.Fatalf("notice kind = %v, want NoticeStale", notice.Kind)
	}
}

func TestModelReloader_RefreshAfterSaveReturnsFreshWhenHealthy(t *testing.T) {
	t.Parallel()
	fake := &fakeWorkspacePorts{
		cockpit: DefaultFixtureReadModel(),
		workspaces: map[readmodel.WorkspaceID]readmodel.WorkspaceReadModel{
			"lab": {ID: "lab", Slug: "lab", State: readmodel.WorkspaceExisting},
		},
	}
	reloader := NewModelReloader(context.Background(), fake, 5*time.Second)
	current := DefaultFixtureReadModel()

	model, notice := reloader.RefreshAfterSave(current, readmodel.WorkspaceReadModel{ID: "lab", Slug: "lab"})

	if model.SelectedWorkspaceID != "lab" {
		t.Fatalf("selected workspace = %q, want lab", model.SelectedWorkspaceID)
	}
	if !notice.IsEmpty() {
		t.Fatalf("notice = %#v, want empty", notice)
	}
	if fake.loadCockpitCalls != 1 || fake.loadWorkspaceCalls != 1 {
		t.Fatalf("calls = cockpit %d workspace %d, want 1/1", fake.loadCockpitCalls, fake.loadWorkspaceCalls)
	}
}

func TestModelReloader_RefreshAfterDeleteFallsBackOnLoadCockpitError(t *testing.T) {
	t.Parallel()
	fake := &fakeWorkspacePorts{loadCockpitErr: errors.New("daemon down")}
	reloader := NewModelReloader(context.Background(), fake, 5*time.Second)
	current := DefaultFixtureReadModel()

	model, notice := reloader.RefreshAfterDelete(current, "dev")

	if model.SelectedWorkspaceID != "lab" {
		t.Fatalf("selected workspace = %q, want lab", model.SelectedWorkspaceID)
	}
	if notice.Kind != readmodel.NoticeStale {
		t.Fatalf("notice kind = %v, want NoticeStale", notice.Kind)
	}
}

func TestModelReloader_RefreshAfterDeleteReturnsFreshWhenHealthy(t *testing.T) {
	t.Parallel()
	fake := &fakeWorkspacePorts{cockpit: DefaultFixtureReadModel()}
	reloader := NewModelReloader(context.Background(), fake, 5*time.Second)
	current := DefaultFixtureReadModel()

	model, notice := reloader.RefreshAfterDelete(current, "dev")

	if model.SelectedWorkspaceID != "lab" {
		t.Fatalf("selected workspace = %q, want lab", model.SelectedWorkspaceID)
	}
	if !notice.IsEmpty() {
		t.Fatalf("notice = %#v, want empty", notice)
	}
}

func TestModelReloader_NilQueryUsesLocalPatch(t *testing.T) {
	t.Parallel()
	reloader := NewModelReloader(context.Background(), nil, 5*time.Second)
	current := DefaultFixtureReadModel()

	model, notice := reloader.RefreshAfterSave(current, readmodel.WorkspaceReadModel{ID: "lab", Slug: "lab"})
	if model.SelectedWorkspaceID != "lab" {
		t.Fatalf("selected workspace = %q, want lab", model.SelectedWorkspaceID)
	}
	if !notice.IsEmpty() {
		t.Fatalf("notice = %#v, want empty", notice)
	}
}

func TestModelReloader_HonorsCanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fake := &fakeWorkspacePorts{}
	reloader := NewModelReloader(ctx, fake, 5*time.Second)
	current := DefaultFixtureReadModel()

	_, _ = reloader.RefreshAfterSave(current, readmodel.WorkspaceReadModel{ID: "prod", Slug: "prod"})
	if fake.loadCockpitCtxErr == nil || !errors.Is(fake.loadCockpitCtxErr, context.Canceled) {
		t.Fatalf("load cockpit context error = %v, want canceled", fake.loadCockpitCtxErr)
	}
}
