package responses

import (
	"bytes"
	"encoding/json"
	"net/textproto"
	"net/url"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/mcp"
)

type responsesMCPProjection struct {
	declaration canonical.ToolDeclaration
	access      mcp.Access
}

func decodeResponsesMCPNamespace(tool responsesToolDefinitionDTO, access mcp.Access) (responsesMCPProjection, error) {
	fail := func(err error) (responsesMCPProjection, error) {
		return responsesMCPProjection{}, err
	}
	label := strings.TrimSpace(tool.ServerLabel)
	if label == "" {
		label = strings.TrimSpace(tool.Name)
	}
	if label == "" {
		return fail(canonical.BadRequest("responses MCP source requires server_label"))
	}
	key, err := canonical.NewRequestToolKey(canonical.ToolKindNamespace, "mcp/"+label)
	if err != nil {
		return fail(canonical.BadRequest("responses MCP server_label is invalid"))
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
		return fail(canonical.BadRequest("responses MCP source requires exactly one of server_url, connector_id, or tunnel_id"))
	}
	if tool.ConnectorID != nil && strings.TrimSpace(*tool.ConnectorID) == "" {
		return fail(canonical.BadRequest("responses MCP connector_id must be non-empty"))
	}
	if tool.TunnelID != nil && strings.TrimSpace(*tool.TunnelID) == "" {
		return fail(canonical.BadRequest("responses MCP tunnel_id must be non-empty"))
	}

	endpoint := strings.TrimSpace(tool.ServerURL)
	if tool.ConnectorID == nil && tool.TunnelID == nil {
		parsed, parseErr := url.Parse(endpoint)
		if parseErr != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return fail(canonical.BadRequest("responses MCP server_url must be an absolute HTTPS URL"))
		}
		if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fail(canonical.BadRequest("responses MCP server_url must not contain userinfo, query, or fragment"))
		}
		endpoint = parsed.String()
	}

	allowed, err := decodeResponsesMCPNames(tool.AllowedTools, true, "allowed_tools")
	if err != nil {
		return fail(err)
	}
	callers, err := decodeResponsesMCPNames(tool.AllowedCallers, false, "allowed_callers")
	if err != nil {
		return fail(err)
	}
	approval, err := decodeResponsesMCPApproval(tool.RequireApproval)
	if err != nil {
		return fail(err)
	}
	loading := canonical.MCPLoadingEager
	if tool.DeferLoading != nil && *tool.DeferLoading {
		loading = canonical.MCPLoadingDeferred
	}

	var source canonical.MCPSource
	switch {
	case tool.ConnectorID != nil:
		source, err = canonical.NewMCPConnectorSource(
			strings.TrimSpace(*tool.ConnectorID), allowed, approval, loading, callers,
		)
	case tool.TunnelID != nil:
		source, err = canonical.NewMCPTunnelSource(
			strings.TrimSpace(*tool.TunnelID), allowed, approval, loading, callers,
		)
	default:
		source, err = canonical.NewMCPURLSource(endpoint, allowed, approval, loading, callers)
	}
	if err != nil {
		return fail(err)
	}
	namespace, err := canonical.NewMCPToolNamespace(key, tool.ServerDescription, source, nil)
	if err != nil {
		return fail(err)
	}

	headers, headerBearer, err := decodeResponsesMCPHeaders(tool.Headers)
	if err != nil {
		return fail(err)
	}
	if tool.Authorization != nil && headerBearer != "" {
		return fail(canonical.BadRequest("responses MCP authorization and Authorization header cannot both be set"))
	}
	if tool.Authorization != nil {
		if strings.TrimSpace(*tool.Authorization) == "" {
			return fail(canonical.BadRequest("responses MCP authorization must be non-empty"))
		}
		access, err = access.WithBearer(key, *tool.Authorization)
		if err != nil {
			return fail(err)
		}
	}
	if len(headers) > 0 {
		access, err = access.WithHeaders(key, headers)
		if err != nil {
			return fail(err)
		}
	}
	return responsesMCPProjection{declaration: namespace, access: access}, nil
}

func decodeResponsesMCPNames(raw json.RawMessage, objectAllowed bool, field string) (canonical.Specified[[]string], error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return canonical.Unspecified[[]string](), nil
	}
	var names []string
	if err := json.Unmarshal(raw, &names); err == nil && names != nil {
		return canonical.Specify(names), nil
	}
	if objectAllowed {
		var selector struct {
			ToolNames []string `json:"tool_names"`
		}
		if err := json.Unmarshal(raw, &selector); err == nil && selector.ToolNames != nil {
			return canonical.Specify(selector.ToolNames), nil
		}
	}
	return canonical.Unspecified[[]string](), canonical.BadRequest("responses MCP " + field + " is invalid")
}

func decodeResponsesMCPApproval(raw json.RawMessage) (canonical.MCPApproval, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return canonical.NewMCPApprovalAlways(), nil
	}
	var policy string
	if err := json.Unmarshal(raw, &policy); err == nil {
		switch policy {
		case "never":
			return canonical.NewMCPApprovalNever(), nil
		case "always":
			return canonical.NewMCPApprovalAlways(), nil
		default:
			return canonical.MCPApproval{}, canonical.BadRequest("responses MCP require_approval is invalid")
		}
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || len(object) == 0 {
		return canonical.MCPApproval{}, canonical.BadRequest("responses MCP require_approval is invalid")
	}
	for name := range object {
		if name != "always" && name != "never" {
			return canonical.MCPApproval{}, canonical.BadRequest("responses MCP require_approval is invalid")
		}
	}
	always, err := decodeResponsesMCPToolFilter(object["always"])
	if err != nil {
		return canonical.MCPApproval{}, err
	}
	never, err := decodeResponsesMCPToolFilter(object["never"])
	if err != nil {
		return canonical.MCPApproval{}, err
	}
	return canonical.NewMCPApprovalFilter(always, never)
}

func decodeResponsesMCPToolFilter(raw json.RawMessage) (*canonical.MCPToolFilter, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, canonical.BadRequest("responses MCP approval filter is invalid")
	}
	for name := range object {
		if name != "tool_names" && name != "read_only" {
			return nil, canonical.BadRequest("responses MCP approval filter is invalid")
		}
	}
	names := canonical.Unspecified[[]string]()
	if value, ok := object["tool_names"]; ok {
		var decoded []string
		if err := json.Unmarshal(value, &decoded); err != nil || decoded == nil {
			return nil, canonical.BadRequest("responses MCP approval filter tool_names is invalid")
		}
		names = canonical.Specify(decoded)
	}
	readOnly := canonical.Unspecified[bool]()
	if value, ok := object["read_only"]; ok {
		var decoded bool
		if err := json.Unmarshal(value, &decoded); err != nil {
			return nil, canonical.BadRequest("responses MCP approval filter read_only is invalid")
		}
		readOnly = canonical.Specify(decoded)
	}
	filter, err := canonical.NewMCPToolFilter(names, readOnly)
	if err != nil {
		return nil, err
	}
	return &filter, nil
}

func decodeResponsesMCPHeaders(raw json.RawMessage) (map[string]string, string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, "", nil
	}
	var headers map[string]string
	if err := json.Unmarshal(raw, &headers); err != nil || headers == nil {
		return nil, "", canonical.BadRequest("responses MCP headers are invalid")
	}
	var bearer string
	for name, value := range headers {
		if textproto.CanonicalMIMEHeaderKey(name) == "" ||
			strings.ContainsAny(value, "\r\n") {
			return nil, "", canonical.BadRequest("responses MCP headers are invalid")
		}
		if !strings.EqualFold(strings.TrimSpace(name), "Authorization") {
			continue
		}
		trimmed := strings.TrimSpace(value)
		if len(trimmed) < len("Bearer ") ||
			!strings.EqualFold(trimmed[:len("Bearer ")], "Bearer ") ||
			strings.TrimSpace(trimmed[len("Bearer "):]) == "" {
			return nil, "", canonical.BadRequest("responses MCP Authorization header must use Bearer authentication")
		}
		if bearer != "" {
			return nil, "", canonical.BadRequest("responses MCP Authorization header is duplicated")
		}
		bearer = strings.TrimSpace(trimmed[len("Bearer "):])
	}
	return headers, bearer, nil
}

func decodeResponsesToolOccurrences(tools []responsesToolDefinitionDTO, scope canonical.ContextScope, subjectPrefix string, changeLog *[]compat.Change, exchangeID string, access mcp.Access) ([]canonical.CanonicalItem, []canonical.ToolDeclaration, mcp.Access, error) {
	declarations := make([]canonical.ToolDeclaration, 0, len(tools))
	ordinary := make([]canonical.ToolDeclaration, 0, len(tools))
	for index, tool := range tools {
		if strings.EqualFold(strings.TrimSpace(tool.Type), "mcp") {
			projection, err := decodeResponsesMCPNamespace(tool, access)
			if err != nil {
				return nil, nil, access, err
			}
			access = projection.access
			declarations = append(declarations, projection.declaration)
			continue
		}
		decoded, err := decodeResponsesToolNode(tool, responsesToolNamespaceContext{subjectPrefix: subjectPrefix, index: index}, canonical.RequestToolsKind, changeLog, exchangeID)
		if err != nil {
			return nil, nil, access, err
		}
		declarations = append(declarations, decoded...)
		ordinary = append(ordinary, decoded...)
	}
	if len(declarations) == 0 {
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
