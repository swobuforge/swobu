package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/routing"
)

// SourceBinding is an operator-wired MCP source. Its credentials and transport
// stay behind Call; the registry exposes only native declarations and a typed
// invocation seam to Workspace ingress.
type SourceBinding struct {
	builtinAdmin bool
	ID           string
	// For Connection-backed bindings, these identify the exact published
	// upstream revision. A request-scoped MCP source does not set them.
	ConnectionID       string
	ConnectionRevision string
	Source             canonical.MCPToolSource
	Call               func(context.Context, string, json.RawMessage) (*sdk.CallToolResult, error)
	NativeTools        map[string]json.RawMessage
	// Close releases the source session owned by this binding, when it has one.
	Close func() error
	// ConfigurationFingerprint identifies the persisted definition used to build
	// this binding without incorporating resolved secret material.
	ConfigurationFingerprint string
	// Validate reports whether the durable source definition still matches this
	// binding. A stale definition is unavailable until a fresh binding is built.
	Validate func() bool
}

// ConnectionCatalog exposes the currently bound, secret-free MCP declarations
// for one reusable Connection. It is the authoring projection used by the
// Connection API; Workspace allowlists are applied only by WorkspaceCatalog.
func ConnectionCatalog(connectionID string, bindings map[string]SourceBinding) ([]CatalogTool, error) {
	binding, ok := bindings[connectionID]
	if !ok || binding.ID != connectionID || binding.Call == nil {
		return nil, fmt.Errorf("MCP Connection %q is unavailable", connectionID)
	}
	if binding.ConnectionID != connectionID || binding.ConnectionRevision == "" {
		return nil, fmt.Errorf("MCP Connection %q has no revision-bound runtime binding", connectionID)
	}
	if binding.Validate != nil && !binding.Validate() {
		return nil, fmt.Errorf("MCP Connection %q changed; restart the daemon to rebuild its binding", connectionID)
	}
	tools := make([]CatalogTool, 0, len(binding.Source.Tools()))
	for _, declaration := range binding.Source.Tools() {
		function, ok := declaration.Function()
		if !ok {
			return nil, fmt.Errorf("MCP Connection %q contains a non-function tool", connectionID)
		}
		remoteName := function.Key().Name()
		tool := CatalogTool{
			Name: remoteName, SourceID: connectionID, Source: function.Key(),
			RemoteName: remoteName, Declaration: declaration.Clone(),
			ConnectionID: binding.ConnectionID, ConnectionRevision: binding.ConnectionRevision,
		}
		if binding.NativeTools != nil {
			native, exists := binding.NativeTools[remoteName]
			if !exists {
				return nil, fmt.Errorf("native MCP descriptor is unavailable")
			}
			tool.NativeDescriptor = append(json.RawMessage(nil), native...)
		}
		tools = append(tools, tool)
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools, nil
}

// OpenSourceBinding opens one URL-backed source through the existing secure SDK
// client path and returns its selected native catalog plus a source-bound call
// seam. The returned binding owns the session for its lifetime.
func OpenSourceBinding(ctx context.Context, source canonical.MCPToolSource, access SourceAccess) (SourceBinding, error) {
	opened, err := openSession(ctx, source, access)
	if err != nil {
		return SourceBinding{}, err
	}
	return sourceBindingFromSession(ctx, opened)
}

func sourceBindingFromSession(ctx context.Context, opened *session) (SourceBinding, error) {
	source := opened.source
	children, _, err := opened.listTools(ctx)
	if err != nil {
		_ = opened.close()
		return SourceBinding{}, err
	}
	declaration, err := canonical.NewMCPToolSource(source.Key(), source.Description(), source.Source(), children)
	if err != nil {
		_ = opened.close()
		return SourceBinding{}, err
	}
	catalog, _ := declaration.MCP()
	return SourceBinding{ID: source.Key().Name(), Source: catalog, NativeTools: opened.nativeTools, Close: opened.close, Call: func(callCtx context.Context, remoteName string, args json.RawMessage) (*sdk.CallToolResult, error) {
		var arguments map[string]json.RawMessage
		if len(args) > 0 {
			if err := json.Unmarshal(args, &arguments); err != nil {
				return nil, err
			}
		}
		return opened.sdk.CallTool(callCtx, &sdk.CallToolParams{Name: remoteName, Arguments: arguments})
	}}, nil
}

// WorkspaceCatalog resolves Connection-backed mounts against revision-frozen
// upstream tool bindings. Unavailable or stale bindings fail closed.
func WorkspaceCatalog(workspace routing.Workspace, bindings map[string]SourceBinding) ([]CatalogTool, error) {
	mounts := workspace.MCPMounts()
	selected := make([]Mount, 0, len(mounts))
	for alias, mount := range mounts {
		binding, ok := bindings[mount.SourceID()]
		if !ok || binding.ID != mount.SourceID() || binding.Call == nil {
			return nil, fmt.Errorf("MCP source %q is unavailable", mount.SourceID())
		}
		if mount.Builtin != "" {
			if mount.Builtin != routing.AdminMCPSource || mount.ConnectionID != "" || !binding.builtinAdmin {
				return nil, fmt.Errorf("MCP builtin source is unavailable")
			}
		} else if binding.builtinAdmin || binding.ConnectionID != mount.ConnectionID || binding.ConnectionRevision == "" {
			return nil, fmt.Errorf("MCP Connection %q has no revision-bound runtime binding", mount.ConnectionID)
		}
		if binding.Validate != nil && !binding.Validate() {
			return nil, fmt.Errorf("MCP source changed; select the catalog again")
		}
		selected = append(selected, Mount{Alias: alias, Source: binding.Source, AllowTools: append([]string(nil), mount.AllowedTools...)})
	}
	catalog, err := BuildCatalog(selected)
	if err != nil {
		return nil, err
	}
	// Registry identity is owned by the mount, not the source's display/key
	// label. Multiple reusable bindings may deliberately share that label.
	for i := range catalog {
		for alias, mount := range mounts {
			if catalog[i].Name == alias+"__"+catalog[i].RemoteName {
				catalog[i].SourceID = mount.SourceID()
				binding := bindings[mount.SourceID()]
				catalog[i].ConnectionID = binding.ConnectionID
				catalog[i].ConnectionRevision = binding.ConnectionRevision
				if binding.NativeTools != nil {
					native, exists := binding.NativeTools[catalog[i].RemoteName]
					if !exists {
						return nil, fmt.Errorf("native MCP descriptor is unavailable")
					}
					catalog[i].NativeDescriptor = append(json.RawMessage(nil), native...)
					if _, err := publishedNativeTool(catalog[i]); err != nil {
						return nil, err
					}
				}
				break
			}
		}
	}
	return catalog, nil
}

// CallWorkspaceTool revalidates a frozen catalog entry against the current
// mount and schema before dispatch. A name cannot silently rebind to another
// source or schema while a call is pending. Caller authorization belongs to
// ingress and must be checked immediately before invoking this effect seam.
func CallWorkspaceTool(ctx context.Context, workspace routing.Workspace, bindings map[string]SourceBinding, frozen CatalogTool, args json.RawMessage) (*sdk.CallToolResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, ToolCallTimeout)
	defer cancel()
	catalog, err := WorkspaceCatalog(workspace, bindings)
	if err != nil {
		return nil, err
	}
	for _, current := range catalog {
		if current.Name != frozen.Name {
			continue
		}
		if current.SourceID != frozen.SourceID || current.Source != frozen.Source || current.RemoteName != frozen.RemoteName || current.ConnectionID != frozen.ConnectionID || current.ConnectionRevision != frozen.ConnectionRevision || !reflect.DeepEqual(current.Declaration, frozen.Declaration) || !reflect.DeepEqual(current.NativeDescriptor, frozen.NativeDescriptor) {
			return nil, fmt.Errorf("MCP tool %q binding changed; select the catalog again", frozen.Name)
		}
		return bindings[current.SourceID].Call(ctx, current.RemoteName, args)
	}
	return nil, fmt.Errorf("MCP tool %q is not published", frozen.Name)
}

// CallWorkspaceToolResult executes the frozen selected binding and preserves
// the provider call's causal identity in a canonical result for model resume.
// The caller must reauthorize the principal and supply current Workspace state
// immediately before entry. Credentials remain confined to SourceBinding.Call.
func CallWorkspaceToolResult(ctx context.Context, workspace routing.Workspace, bindings map[string]SourceBinding, frozen CatalogTool, call canonical.ToolCallItem) (canonical.CanonicalItem, error) {
	key, err := canonical.NewToolKey("workspace/mcp", canonical.ToolKindFunction, frozen.Name)
	if err != nil || call.Tool() != key {
		return canonical.CanonicalItem{}, canonical.BadRequest("Workspace tool call does not match the selected binding")
	}
	input, ok := call.Input().Object()
	if !ok {
		return canonical.CanonicalItem{}, canonical.BadRequest("Workspace tool call requires object arguments")
	}
	result, err := CallWorkspaceTool(ctx, workspace, bindings, frozen, json.RawMessage(input.String()))
	if err != nil {
		return canonical.CanonicalItem{}, err
	}
	parts, isError, err := projectToolResult(frozen.RemoteName, result)
	if err != nil {
		return canonical.CanonicalItem{}, err
	}
	return canonical.NewToolResultItem(call.CallID(), parts, isError)
}
