package exchange

import (
	"context"

	"github.com/swobuforge/swobu/internal/routing"
)

// WorkspaceLookup resolves the durable workspace selected by request URL.
type WorkspaceLookup interface {
	GetWorkspace(context.Context, routing.WorkspaceSlug) (routing.Workspace, error)
}
