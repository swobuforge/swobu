package responses

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

// ProviderRequestTool is one typed Responses tool declaration before exact-
// provider spelling and the single JSON serialization boundary.
type ProviderRequestTool struct {
	Type              string                `json:"type"`
	Name              string                `json:"name,omitempty"`
	Description       string                `json:"description,omitempty"`
	Parameters        any                   `json:"parameters,omitempty"`
	Strict            *bool                 `json:"strict,omitempty"`
	Format            any                   `json:"format,omitempty"`
	Tools             []ProviderRequestTool `json:"tools,omitempty"`
	Execution         string                `json:"execution,omitempty"`
	ServerLabel       string                `json:"server_label,omitempty"`
	ServerDescription string                `json:"server_description,omitempty"`
	ServerURL         string                `json:"server_url,omitempty"`
	ConnectorID       string                `json:"connector_id,omitempty"`
	TunnelID          string                `json:"tunnel_id,omitempty"`
	AllowedTools      *[]string             `json:"allowed_tools,omitempty"`
	AllowedCallers    *[]string             `json:"allowed_callers,omitempty"`
	RequireApproval   any                   `json:"require_approval,omitempty"`
	Headers           map[string]string     `json:"headers,omitempty"`
	Authorization     string                `json:"authorization,omitempty"`
	DeferLoading      *bool                 `json:"defer_loading,omitempty"`
}

func encodeResponsesTools(tools []canonical.ToolDeclaration, names wire.ToolNames, access mcp.Access, changeLog *[]compat.Change, exchangeID string) ([]ProviderRequestTool, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	flattened, err := wire.PrepareFlatToolSet(tools, func(tool canonical.ToolDeclaration) (string, error) {
		return responsesFlatToolIdentity(tool, names)
	})
	if err != nil {
		return nil, err
	}
	if flattened.RemovedNamespaces > 0 {
		if err := appendResponsesRequestChange(changeLog, exchangeID, canonical.RequestTools, compat.Approximation); err != nil {
			return nil, err
		}
	}
	if flattened.OmittedMCP > 0 {
		if err := appendResponsesRequestChange(changeLog, exchangeID, canonical.RequestToolsKind, compat.Omission); err != nil {
			return nil, err
		}
	}
	tools = flattened.Declarations
	out := make([]ProviderRequestTool, 0, len(tools))
	for _, tool := range tools {
		wireTool, err := encodeResponsesTool(tool, names, access)
		if err != nil {
			return nil, err
		}
		out = append(out, wireTool)
	}
	return out, nil
}

func responsesFlatToolIdentity(tool canonical.ToolDeclaration, names wire.ToolNames) (string, error) {
	switch tool.Kind() {
	case canonical.ToolKindFunction, canonical.ToolKindCustom:
		name, err := responsesToolWireName(tool, names)
		return string(tool.Kind()) + "\x00" + strings.TrimSpace(name), err
	case canonical.ToolKindWebSearch, canonical.ToolKindDiscovery:
		return string(tool.Kind()), nil
	default:
		return "", provider.IncompatibleCapability(canonical.RequestToolsKind, canonical.ToolOccurrence(tool.Key()), "Responses cannot represent this canonical tool declaration type")
	}
}

func encodeResponsesTool(tool canonical.ToolDeclaration, names wire.ToolNames, access mcp.Access) (ProviderRequestTool, error) {
	if tool.Kind() == "" {
		return ProviderRequestTool{}, canonical.BadRequest("response request tool declarations are invalid")
	}
	if decl, ok := tool.Function(); ok {
		return encodeResponsesFunctionTool(tool, decl, names)
	}
	if decl, ok := tool.Custom(); ok {
		return encodeResponsesCustomTool(tool, decl, names)
	}
	if tool.Kind() == canonical.ToolKindWebSearch {
		return ProviderRequestTool{Type: canonical.ToolTypeWebSearch}, nil
	}
	if source, ok := tool.MCP(); ok {
		return encodeResponsesMCPTool(source, access)
	}
	if discovery, ok := tool.Discovery(); ok {
		parameters, err := responsesToolParametersFromSchema(discovery.InputSchema())
		if err != nil {
			return ProviderRequestTool{}, err
		}
		execution := "server"
		if discovery.Executor() == canonical.DiscoveryExecutorClient {
			execution = "client"
		}
		return ProviderRequestTool{Type: "tool_search", Description: discovery.Description(), Parameters: parameters, Execution: execution}, nil
	}
	return ProviderRequestTool{}, provider.IncompatibleCapability(canonical.RequestToolsKind, canonical.ToolOccurrence(tool.Key()), "Responses cannot represent this canonical tool declaration type")
}

func encodeResponsesMCPTool(declaration canonical.MCPToolSource, access mcp.Access) (ProviderRequestTool, error) {
	source := declaration.Source()
	if source.Approval().Kind() != canonical.MCPApprovalNever {
		return ProviderRequestTool{}, provider.IncompatibleCapability(canonical.RequestToolsKind, canonical.ToolOccurrence(declaration.Key()), "Responses MCP approval requires an approval request/response lifecycle")
	}
	wire := ProviderRequestTool{
		Type:              "mcp",
		ServerLabel:       declaration.Key().Name(),
		ServerDescription: declaration.Description(),
		RequireApproval:   "never",
	}
	switch source.Kind() {
	case canonical.MCPSourceURL:
		wire.ServerURL, _ = source.URL()
	case canonical.MCPSourceConnectorID:
		wire.ConnectorID, _ = source.ConnectorID()
	case canonical.MCPSourceTunnelID:
		wire.TunnelID, _ = source.TunnelID()
	default:
		return ProviderRequestTool{}, canonical.InternalError("canonical MCP source kind is invalid")
	}
	if allowed, specified := source.AllowedTools().Get(); specified {
		copied := append([]string(nil), allowed...)
		wire.AllowedTools = &copied
	}
	if callers, specified := source.AllowedCallers().Get(); specified {
		copied := append([]string(nil), callers...)
		wire.AllowedCallers = &copied
	}
	if source.Loading() == canonical.MCPLoadingDeferred {
		deferred := true
		wire.DeferLoading = &deferred
	}
	private := access.ForSource(declaration.Key())
	wire.Authorization = private.Authorization
	wire.Headers = private.Headers
	return wire, nil
}

func responsesNamespaceLeafName(name string) string {
	if index := strings.LastIndex(name, "/"); index >= 0 {
		return name[index+1:]
	}
	return name
}

func responsesClientNamespace(tool canonical.ToolKey) string {
	return responsesClientNamespaceValue(tool.Namespace())
}

func responsesClientNamespaceValue(namespace string) string {
	if namespace == canonical.ToolNamespaceRequest {
		return ""
	}
	return responsesNamespaceLeafName(namespace)
}

func resolveResponsesFunctionCall(tools []canonical.ToolDeclaration, namespace, name string) (canonical.ToolDeclaration, error) {
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	if namespace == "" {
		declaration, _, err := canonical.ResolveToolDeclarationByName(tools, name, canonical.ToolTypeFunction)
		return declaration, err
	}
	for _, tool := range tools {
		group, ok := tool.Namespace()
		if !ok || responsesNamespaceLeafName(group.Key().Name()) != namespace {
			continue
		}
		declaration, _, err := canonical.ResolveToolDeclarationByName(group.Tools(), name, canonical.ToolTypeFunction)
		return declaration, err
	}
	return canonical.ToolDeclaration{}, canonical.BadRequest("Responses function call references an unknown namespace")
}

func encodeResponsesFunctionTool(declaration canonical.ToolDeclaration, decl canonical.FunctionTool, names wire.ToolNames) (ProviderRequestTool, error) {
	name, err := responsesToolWireName(declaration, names)
	if err != nil {
		return ProviderRequestTool{}, err
	}
	name = strings.TrimSpace(name) // swobu:io-string source=boundary
	if name == "" {
		return ProviderRequestTool{}, canonical.BadRequest("response request tool declarations require a name")
	}
	parameters, err := responsesToolParametersFromSchema(decl.InputSchema())
	if err != nil {
		return ProviderRequestTool{}, err
	}
	wire := ProviderRequestTool{
		Type:        "function",
		Name:        name,
		Description: strings.TrimSpace(decl.Description()), // swobu:io-string source=boundary
		Parameters:  parameters,
	}
	if strict, ok := decl.Strict().Get(); ok {
		wire.Strict = &strict
	}
	return wire, nil
}

func encodeResponsesCustomTool(declaration canonical.ToolDeclaration, decl canonical.CustomTool, names wire.ToolNames) (ProviderRequestTool, error) {
	name, err := responsesToolWireName(declaration, names)
	if err != nil {
		return ProviderRequestTool{}, err
	}
	name = strings.TrimSpace(name) // swobu:io-string source=boundary
	if name == "" {
		return ProviderRequestTool{}, canonical.BadRequest("response request custom tool declarations require a name")
	}
	format, err := responsesToolFormatFromCanonical(decl.Format())
	if err != nil {
		return ProviderRequestTool{}, err
	}
	return ProviderRequestTool{
		Type:        "custom",
		Name:        name,
		Description: strings.TrimSpace(decl.Description()), // swobu:io-string source=boundary
		Format:      format,
	}, nil
}

func responsesToolParametersFromSchema(schema canonical.ToolSchema) (any, error) {
	raw := strings.TrimSpace(schema.RawObject()) // swobu:io-string source=domain
	if raw == "" {
		return nil, canonical.BadRequest("response request tool declarations require input_schema")
	}
	if _, err := sse.DecodeJSONObject(json.RawMessage(raw), "response request tool declaration input_schema is invalid"); err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func responsesToolFormatFromCanonical(format canonical.ToolFormat) (any, error) {
	raw := strings.TrimSpace(format.RawObject()) // swobu:io-string source=domain
	if raw == "" {
		return nil, canonical.BadRequest("response request tool declarations require format")
	}
	if _, err := sse.DecodeJSONObject(json.RawMessage(raw), "response request tool declaration format is invalid"); err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func encodeToolChoice(policy canonical.ToolPolicy, tools []canonical.ToolDeclaration, names wire.ToolNames, changeLog *[]compat.Change, exchangeID string) (any, error) {
	if err := policy.ValidateForTools(tools); err != nil {
		return nil, err
	}
	switch policy.Mode {
	case canonical.ToolPolicyNone:
		if len(tools) == 0 {
			return nil, nil
		}
		return "none", nil
	case canonical.ToolPolicyAuto:
		return "auto", nil
	case canonical.ToolPolicyRequired:
		if len(tools) == 0 {
			return nil, canonical.BadRequest("response request tool_choice required requires at least one tool")
		}
		return "required", nil
	case canonical.ToolPolicySpecific:
		specific, ok := policy.SpecificID()
		if !ok {
			return nil, canonical.BadRequest("response request tool_choice specific requires a tool id")
		}
		specificType := string(specific.Kind())
		decl, resolvedType, err := canonical.ResolveToolDeclarationByKey(tools, specific, specificType)
		if err != nil {
			return nil, err
		}
		if resolvedType == canonical.ToolTypeWebSearch {
			return map[string]any{"type": canonical.ToolTypeWebSearch}, nil
		}
		name, err := responsesToolWireName(decl, names)
		if err != nil {
			return nil, err
		}
		if resolvedType == "" {
			resolvedType = "function"
		}
		return map[string]any{
			"type": resolvedType,
			"name": name,
		}, nil
	default:
		return nil, canonical.BadRequest("response request tool_choice is invalid")
	}
}

type responsesToolNamespaceContext struct {
	path          []string
	subjectPrefix string
	index         int
}

func cloneBoolPointer(ptr *bool) *bool {
	if ptr == nil {
		return nil
	}
	cloned := *ptr
	return &cloned
}
