package ports

import (
	"context"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

// WorkspaceQueries loads Cockpit navigation and workspace snapshots.
type WorkspaceQueries interface {
	LoadCockpit(ctx context.Context) (readmodel.CockpitReadModel, error)
	LoadWorkspace(ctx context.Context, id readmodel.WorkspaceID) (readmodel.WorkspaceReadModel, error)
}

// WorkspaceCommands mutates workspace-level configuration.
type WorkspaceCommands interface {
	RenameWorkspace(ctx context.Context, request RenameWorkspaceRequest) (readmodel.WorkspaceReadModel, error)
	DeleteWorkspace(ctx context.Context, request DeleteWorkspaceRequest) error
}

// RenameWorkspaceRequest describes a persisted workspace rename. Draft naming is
// local Cockpit state and does not cross this command boundary.
type RenameWorkspaceRequest struct {
	ID   readmodel.WorkspaceID
	Slug string
}

// DeleteWorkspaceRequest names the workspace to delete after confirmation.
type DeleteWorkspaceRequest struct {
	ID readmodel.WorkspaceID
}
