package ports

import (
	"context"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

// ActivityQueries loads recent request evidence for the Cockpit activity
// section.
type ActivityQueries interface {
	ListActivity(ctx context.Context, request ListActivityRequest) (readmodel.ActivityReadModel, error)
}

// ListActivityRequest scopes recent activity to a workspace.
type ListActivityRequest struct {
	WorkspaceID readmodel.WorkspaceID
	Limit       int
}
