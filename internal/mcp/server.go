package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/swobuforge/swobu/internal/routing"
)

// WorkspaceServerConfig supplies the authority and source-bound execution
// seams for the Workspace MCP endpoint. The SDK transport never routes Node
// Control HTTP; a trusted builtin source can invoke canonical checked commands
// using original Workspace admission at its owning effect boundary.
type WorkspaceServerConfig struct {
	Authorize func(*http.Request, string) bool
	Catalog   func(context.Context, string) ([]CatalogTool, error)
	Call      func(context.Context, string, CatalogTool, json.RawMessage) (*sdk.CallToolResult, error)
}

// NewWorkspaceMCPHandler constructs the exact Workspace MCP endpoint. It
// rejects ambiguous paths and unauthorized callers before handing the request
// to the official Streamable HTTP transport.
func NewWorkspaceMCPHandler(config WorkspaceServerConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		workspace, ok := workspaceMCPPath(r.URL.Path)
		if !ok || r.Method != http.MethodPost || (r.URL.RawPath != "" && strings.Contains(strings.ToLower(r.URL.RawPath), "%2f")) || config.Authorize == nil || !config.Authorize(r, workspace) {
			http.NotFound(w, r)
			return
		}
		if !workspaceMCPOriginAllowed(r) {
			http.Error(w, "MCP Origin is not trusted", http.StatusForbidden)
			return
		}
		if config.Catalog == nil || config.Call == nil {
			http.Error(w, "MCP service unavailable", http.StatusServiceUnavailable)
			return
		}
		tools, err := config.Catalog(r.Context(), workspace)
		if errors.Is(err, routing.ErrServiceDisabled) {
			http.Error(w, "Workspace MCP Service disabled", http.StatusNotImplemented)
			return
		}
		if err != nil {
			http.Error(w, "MCP catalog unavailable", http.StatusServiceUnavailable)
			return
		}
		byName := make(map[string]CatalogTool, len(tools))
		server := sdk.NewServer(&sdk.Implementation{Name: "swobu-workspace", Version: "v1"}, &sdk.ServerOptions{GetSessionID: func() string { return "" }})
		for _, catalogTool := range tools {
			if _, exists := byName[catalogTool.Name]; exists || catalogTool.Name == "" {
				http.Error(w, "MCP catalog is invalid", http.StatusInternalServerError)
				return
			}
			byName[catalogTool.Name] = catalogTool
			tool, err := publishedNativeTool(catalogTool)
			if err != nil {
				http.Error(w, "MCP catalog is invalid", http.StatusInternalServerError)
				return
			}
			selected := catalogTool
			sdkServerAddTool(server, tool, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
				ctx, cancel := context.WithCancel(ctx)
				stop := context.AfterFunc(r.Context(), cancel)
				defer stop()
				defer cancel()
				if req == nil || req.Params == nil || req.Params.Name != selected.Name {
					return nil, fmt.Errorf("MCP tool binding mismatch")
				}
				result, err := config.Call(ctx, workspace, selected, req.Params.Arguments)
				if err != nil {
					return nil, err
				}
				if err := validateNativeResult(result); err != nil {
					return nil, err
				}
				return result, nil
			})
		}
		transport := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, &sdk.StreamableHTTPOptions{Stateless: true})
		transport.ServeHTTP(&workspaceMCPResponseWriter{ResponseWriter: w}, r)
	})
}

type workspaceMCPResponseWriter struct {
	http.ResponseWriter
}

func (writer *workspaceMCPResponseWriter) WriteHeader(status int) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *workspaceMCPResponseWriter) Write(body []byte) (int, error) {
	writer.Header().Set("Cache-Control", "no-store")
	return writer.ResponseWriter.Write(body)
}

func (writer *workspaceMCPResponseWriter) Flush() {
	writer.Header().Set("Cache-Control", "no-store")
	_ = http.NewResponseController(writer.ResponseWriter).Flush()
}

func (writer *workspaceMCPResponseWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

func workspaceMCPOriginAllowed(request *http.Request) bool {
	origins := request.Header.Values("Origin")
	if len(origins) == 0 {
		return true
	}
	if len(origins) != 1 {
		return false
	}
	origin, err := url.Parse(origins[0])
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}
	return err == nil && origin.Scheme == scheme && origin.Host != "" && origin.Host == request.Host && origin.User == nil && origin.Opaque == "" && origin.Path == "" && origin.RawPath == "" && origin.RawQuery == "" && !origin.ForceQuery && origin.Fragment == "" && origin.RawFragment == ""
}

// sdkServerAddTool isolates the SDK's non-generic registration method from the
// workspace handler's catalog loop.
func sdkServerAddTool(server *sdk.Server, tool *sdk.Tool, call func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error)) {
	server.AddTool(tool, sdk.ToolHandler(call))
}

func workspaceMCPPath(path string) (string, bool) {
	const prefix = "/workspaces/"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, "/mcp") {
		return "", false
	}
	slug := strings.TrimSuffix(strings.TrimPrefix(path, prefix), "/mcp")
	if slug == "" || strings.Contains(slug, "/") || strings.Contains(slug, "%") || slug == "." || slug == ".." {
		return "", false
	}
	return slug, true
}
