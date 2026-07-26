package responses

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/mcp"
)

func decodeResponsesMCPNamespace(tool responsesToolDefinitionDTO, access mcp.Access) (canonical.ToolDeclaration, mcp.Access, error) {
	label := strings.TrimSpace(tool.ServerLabel)
	if label == "" {
		label = strings.TrimSpace(tool.Name)
	}
	if label == "" {
		return canonical.ToolDeclaration{}, access, canonical.BadRequest("responses MCP source requires server_label")
	}
	sourceSelectors := 0
	if strings.TrimSpace(tool.ServerURL) != "" {
		sourceSelectors++
	}
	if tool.ConnectorID != nil {
		sourceSelectors++
	}
	if tool.TunnelID != nil {
		sourceSelectors++
	}
	if sourceSelectors != 1 {
		return canonical.ToolDeclaration{}, access, canonical.BadRequest("responses MCP source requires exactly one of server_url, connector_id, or tunnel_id")
	}
	if tool.ConnectorID != nil {
		return canonical.ToolDeclaration{}, access, canonical.NotImplemented("Swobu does not implement managed MCP connector IDs")
	}
	if tool.TunnelID != nil {
		return canonical.ToolDeclaration{}, access, canonical.NotImplemented("Swobu does not implement MCP tunnel IDs")
	}
	if tool.DeferLoading != nil && *tool.DeferLoading {
		return canonical.ToolDeclaration{}, access, canonical.NotImplemented("Swobu does not implement deferred MCP loading")
	}
	if raw := bytes.TrimSpace(tool.AllowedCallers); len(raw) > 0 &&
		!bytes.Equal(raw, []byte("null")) {
		return canonical.ToolDeclaration{}, access, canonical.NotImplemented("Swobu does not implement restricted MCP callers")
	}
	if !responsesMCPHeadersOmitted(tool.Headers) {
		return canonical.ToolDeclaration{}, access, canonical.NotImplemented("Swobu does not implement MCP request headers")
	}
	parsed, err := url.Parse(strings.TrimSpace(tool.ServerURL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return canonical.ToolDeclaration{}, access, canonical.BadRequest("responses MCP server_url must be an absolute HTTPS URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return canonical.ToolDeclaration{}, access, canonical.BadRequest("responses MCP server_url must not contain userinfo, query, or fragment")
	}
	allowed := canonical.Unspecified[[]string]()
	if raw := bytes.TrimSpace(tool.AllowedTools); len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		var names []string
		if err := json.Unmarshal(raw, &names); err != nil {
			return canonical.ToolDeclaration{}, access, canonical.NotImplemented("Swobu implements only explicit MCP allowed_tools arrays")
		}
		allowed = canonical.Specify(names)
	}
	rawApproval := bytes.TrimSpace(tool.RequireApproval)
	var approval string
	if len(rawApproval) == 0 || bytes.Equal(rawApproval, []byte("null")) ||
		json.Unmarshal(rawApproval, &approval) != nil || approval != "never" {
		return canonical.ToolDeclaration{}, access, canonical.NotImplemented("Swobu requires explicit no-approval MCP execution")
	}
	key, err := canonical.NewRequestToolKey(canonical.ToolKindNamespace, "mcp/"+label)
	if err != nil {
		return canonical.ToolDeclaration{}, access, canonical.BadRequest("responses MCP server_label is invalid")
	}
	source, err := canonical.NewMCPSource(parsed.String(), allowed)
	if err != nil {
		return canonical.ToolDeclaration{}, access, err
	}
	namespace, err := canonical.NewMCPToolNamespace(key, tool.ServerDescription, source, nil)
	if err != nil {
		return canonical.ToolDeclaration{}, access, err
	}
	if tool.Authorization != nil {
		access, err = access.WithBearer(key, *tool.Authorization)
		if err != nil {
			return canonical.ToolDeclaration{}, access, err
		}
	}
	return namespace, access, nil
}

func responsesMCPHeadersOmitted(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return true
	}
	var headers map[string]json.RawMessage
	return json.Unmarshal(raw, &headers) == nil && len(headers) == 0
}

func decodeResponsesToolOccurrences(tools []responsesToolDefinitionDTO, scope canonical.ContextScope, sink compat.Sink, exchangeID string, preserveEmpty bool, access mcp.Access) ([]canonical.CanonicalItem, []canonical.ToolDeclaration, mcp.Access, error) {
	declarations := make([]canonical.ToolDeclaration, 0, len(tools))
	ordinary := make([]canonical.ToolDeclaration, 0, len(tools))
	for index, tool := range tools {
		if strings.EqualFold(strings.TrimSpace(tool.Type), "mcp") {
			namespace, updated, err := decodeResponsesMCPNamespace(tool, access)
			if err != nil {
				return nil, nil, access, err
			}
			access = updated
			declarations = append(declarations, namespace)
			continue
		}
		decoded, err := decodeResponsesToolNode(tool, responsesToolNamespaceContext{index: index}, sink, exchangeID)
		if err != nil {
			return nil, nil, access, err
		}
		declarations = append(declarations, decoded...)
		ordinary = append(ordinary, decoded...)
	}
	if len(declarations) == 0 && !preserveEmpty {
		return nil, ordinary, access, nil
	}
	set, err := canonical.NewToolSet(declarations)
	if err != nil {
		return nil, nil, access, err
	}
	item, err := canonical.NewToolDeclarationsItem(set, scope)
	if err != nil {
		return nil, nil, access, err
	}
	return []canonical.CanonicalItem{item}, ordinary, access, nil
}
