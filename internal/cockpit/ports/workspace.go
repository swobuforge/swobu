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
	SaveWorkspace(ctx context.Context, request SaveWorkspaceRequest) (readmodel.WorkspaceReadModel, error)
	DeleteWorkspace(ctx context.Context, request DeleteWorkspaceRequest) error
}

// SaveWorkspaceRequest describes a create or edit request for a Cockpit
// workspace.
type SaveWorkspaceRequest struct {
	ID   readmodel.WorkspaceID
	Slug string
}

// DeleteWorkspaceRequest names the workspace to delete after confirmation.
type DeleteWorkspaceRequest struct {
	ID readmodel.WorkspaceID
}
