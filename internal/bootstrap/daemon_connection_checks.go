package bootstrap

import (
	"context"
	"errors"

	"github.com/swobuforge/swobu/internal/adapters/outbound/connectioncheck"
	"github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/search"
)

// checkDaemonConnection dispatches the sealed spec, with no Workspace mount,
// runtime catalog publication or credential mutation. The application command
// owns admission and revision checks before and after these bounded effects.
func checkDaemonConnection(ctx context.Context, value connections.Connection) (connections.Check, error) {
	resolve := credentials.NewResolver().ResolveScopedCredential
	switch value.Spec().(type) {
	case connections.ModelSpec:
		return connectioncheck.Models(ctx, value, resolve)
	case connections.MCPHTTPSpec:
		return mcp.CheckHTTPConnection(ctx, value, resolve)
	case connections.MCPStdioSpec:
		return mcp.CheckStdioConnection(ctx, value, resolve)
	case connections.SearchSpec:
		return search.CheckConnection(ctx, value, resolve)
	default:
		return connections.Check{}, errors.New("unsupported Connection check")
	}
}
