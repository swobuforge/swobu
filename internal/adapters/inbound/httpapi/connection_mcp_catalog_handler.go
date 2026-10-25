package httpapi

import (
	"context"
	"net/http"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/mcp"
)

// NewConnectionMCPCatalogHandler exposes a reviewed Connection's current,
// secret-free MCP declarations for authoring and verification.
func NewConnectionMCPCatalogHandler(catalog func(context.Context, connectionidentity.ID) ([]mcp.CatalogTool, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		id, err := connectionidentity.Parse(r.PathValue("id"))
		if err != nil {
			connectionProblem(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "Invalid Connection ID.")
			return
		}
		if catalog == nil {
			connectionProblem(w, r, http.StatusServiceUnavailable, "UNAVAILABLE", "MCP catalog is unavailable.")
			return
		}
		tools, err := catalog(r.Context(), id)
		if err != nil {
			connectionProblem(w, r, http.StatusNotFound, "NOT_FOUND", "MCP catalog unavailable.")
			return
		}
		type item struct {
			Name               string `json:"name"`
			RemoteName         string `json:"remote_name"`
			ConnectionID       string `json:"connection_id"`
			ConnectionRevision string `json:"connection_revision"`
		}
		out := make([]item, 0, len(tools))
		for _, tool := range tools {
			out = append(out, item{tool.Name, tool.RemoteName, tool.ConnectionID, tool.ConnectionRevision})
		}
		connectionJSON(w, out, "")
	})
}
