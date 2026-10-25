package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/swobuforge/swobu/internal/mcp"
)

// NewWorkspaceToolsHandler exposes the derived, secret-free MCP catalog on
// Node Control. It never becomes an execution or credential authority.
func NewWorkspaceToolsHandler(catalog func(context.Context, string) ([]mcp.CatalogTool, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		workspace := r.PathValue("workspace")
		tools, err := catalog(r.Context(), workspace)
		if err != nil {
			http.Error(w, "MCP catalog unavailable", http.StatusNotFound)
			return
		}
		type item struct {
			Name       string `json:"name"`
			Source     string `json:"source"`
			RemoteName string `json:"remote_name"`
		}
		out := make([]item, 0, len(tools))
		for _, tool := range tools {
			out = append(out, item{Name: tool.Name, Source: tool.Source.String(), RemoteName: tool.RemoteName})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	})
}
