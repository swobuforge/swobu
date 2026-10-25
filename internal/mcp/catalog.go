package mcp

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/routing"
)

var publishedToolPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

// Mount is the explicit Workspace attachment policy for one resolved MCP
// source. AllowTools is a closed allowlist; an empty list publishes nothing.
type Mount struct {
	Alias      string
	Source     canonical.MCPToolSource
	AllowTools []string
}

// CatalogTool is the stable native catalog binding. The canonical declaration
// retains the upstream schema and source identity; Name is the published,
// deterministic alias used by tools/list and tools/call.
type CatalogTool struct {
	Name               string
	SourceID           string
	Source             canonical.ToolKey
	RemoteName         string
	Declaration        canonical.ToolDeclaration
	NativeDescriptor   json.RawMessage
	ConnectionID       string
	ConnectionRevision string
}

// BuildCatalog derives a deterministic selected catalog from explicit mounts.
// It never discovers new tools, permits wildcard selection, or silently
// resolves collisions.
func BuildCatalog(mounts []Mount) ([]CatalogTool, error) {
	result := make([]CatalogTool, 0)
	seen := map[string]struct{}{}
	for _, mount := range mounts {
		alias := mount.Alias
		if err := routing.ValidateMCPMountAlias(alias); err != nil {
			return nil, fmt.Errorf("invalid MCP mount alias %q", mount.Alias)
		}
		allowed := map[string]struct{}{}
		resolved := map[string]struct{}{}
		for _, name := range mount.AllowTools {
			if name == "" || name == "*" {
				return nil, fmt.Errorf("MCP mount %q has invalid allowlist entry", alias)
			}
			allowed[name] = struct{}{}
		}
		for _, declaration := range mount.Source.Tools() {
			function, ok := declaration.Function()
			if !ok {
				return nil, fmt.Errorf("MCP mount %q contains a non-function tool", alias)
			}
			remoteName := function.Key().Name()
			if _, ok := allowed[remoteName]; !ok {
				continue
			}
			published := alias + "__" + remoteName
			if !publishedToolPattern.MatchString(published) {
				return nil, fmt.Errorf("invalid MCP published tool name %q", published)
			}
			if _, ok := seen[published]; ok {
				return nil, fmt.Errorf("MCP published tool collision %q", published)
			}
			seen[published] = struct{}{}
			resolved[remoteName] = struct{}{}
			result = append(result, CatalogTool{Name: published, SourceID: mount.Source.Key().Name(), Source: mount.Source.Key(), RemoteName: remoteName, Declaration: declaration.Clone()})
		}
		missing := make([]string, 0)
		for name := range allowed {
			if _, exists := resolved[name]; !exists {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			return nil, fmt.Errorf("MCP mount %q selected unavailable tools: %s", alias, strings.Join(missing, ", "))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// SelectCatalog returns the frozen subset named by a caller's explicit opt-in.
// Empty selection is distinct from selecting every tool; unknown names fail
// closed so an LLM cannot guess a hidden catalog entry.
func SelectCatalog(catalog []CatalogTool, names []string) ([]CatalogTool, error) {
	if len(names) == 0 {
		return nil, nil
	}
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("MCP selection contains an empty tool name")
		}
		if _, exists := allowed[name]; exists {
			return nil, fmt.Errorf("MCP selection duplicates %q", name)
		}
		allowed[name] = struct{}{}
	}
	out := make([]CatalogTool, 0, len(names))
	for _, tool := range catalog {
		if _, ok := allowed[tool.Name]; ok {
			out = append(out, tool)
		}
	}
	if len(out) != len(allowed) {
		return nil, fmt.Errorf("MCP selection contains an unpublished tool")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ProjectSelectedDeclarations converts an explicit catalog selection into the
// canonical function declarations consumed by protocol codecs. It preserves
// declaration schemas and never projects the source's credential or transport.
func ProjectSelectedDeclarations(catalog []CatalogTool, names []string) ([]canonical.ToolDeclaration, error) {
	selected, err := SelectCatalog(catalog, names)
	if err != nil {
		return nil, err
	}
	declarations := make([]canonical.ToolDeclaration, 0, len(selected))
	for _, tool := range selected {
		function, ok := tool.Declaration.Function()
		if !ok {
			return nil, fmt.Errorf("MCP catalog entry %q is not a function declaration", tool.Name)
		}
		key, err := canonical.NewToolKey("workspace/mcp", canonical.ToolKindFunction, tool.Name)
		if err != nil {
			return nil, err
		}
		projected, err := canonical.NewFunctionTool(key, function.Description(), function.InputSchema(), function.SchemaContract())
		if err != nil {
			return nil, err
		}
		declarations = append(declarations, projected)
	}
	return declarations, nil
}
