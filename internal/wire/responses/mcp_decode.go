package responses

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/mcp"
)

type responsesMCPProjection struct {
	declaration canonical.ToolDeclaration
	access      mcp.Access
	drop        bool
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

	parsed, err := url.Parse(strings.TrimSpace(tool.ServerURL))
	if tool.ConnectorID == nil && tool.TunnelID == nil {
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return fail(canonical.BadRequest("responses MCP server_url must be an absolute HTTPS URL"))
		}
		if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fail(canonical.BadRequest("responses MCP server_url must not contain userinfo, query, or fragment"))
		}
	}

	unsupported := tool.ConnectorID != nil || tool.TunnelID != nil ||
		(tool.DeferLoading != nil && *tool.DeferLoading)
	if raw := bytes.TrimSpace(tool.AllowedCallers); len(raw) > 0 &&
		!bytes.Equal(raw, []byte("null")) {
		var callers []string
		if err := json.Unmarshal(raw, &callers); err != nil || callers == nil {
			return fail(canonical.BadRequest("responses MCP allowed_callers is invalid"))
		}
		unsupported = true
	}
	headerBearer, customHeaders, err := responsesMCPHeaderBearer(tool.Headers)
	if err != nil {
		return fail(err)
	}
	if tool.Authorization != nil && headerBearer != "" {
		return fail(canonical.BadRequest("responses MCP authorization and Authorization header cannot both be set"))
	}
	if tool.Authorization != nil && strings.TrimSpace(*tool.Authorization) == "" {
		return fail(canonical.BadRequest("responses MCP authorization must be non-empty"))
	}
	unsupported = unsupported || customHeaders

	allowed := canonical.Unspecified[[]string]()
	if raw := bytes.TrimSpace(tool.AllowedTools); len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		var names []string
		if err := json.Unmarshal(raw, &names); err != nil {
			var selector struct {
				ToolNames []string `json:"tool_names"`
			}
			if objectErr := json.Unmarshal(raw, &selector); objectErr != nil || selector.ToolNames == nil {
				return fail(canonical.BadRequest("responses MCP allowed_tools is invalid"))
			}
			names = selector.ToolNames
		}
		allowed = canonical.Specify(names)
	}

	rawApproval := bytes.TrimSpace(tool.RequireApproval)
	switch {
	case len(rawApproval) == 0 || bytes.Equal(rawApproval, []byte("null")):
		unsupported = true
	default:
		var approval string
		if err := json.Unmarshal(rawApproval, &approval); err == nil {
			switch approval {
			case "never":
			case "always":
				unsupported = true
			default:
				return fail(canonical.BadRequest("responses MCP require_approval is invalid"))
			}
			break
		}
		var approvalPolicy map[string]any
		if err := json.Unmarshal(rawApproval, &approvalPolicy); err != nil || len(approvalPolicy) == 0 {
			return fail(canonical.BadRequest("responses MCP require_approval is invalid"))
		}
		unsupported = true
	}

	// Unsupported authority is inseparable from the MCP declaration. Erasing
	// the whole occurrence preserves independent siblings without inventing a
	// weaker remote execution contract.
	if unsupported {
		return responsesMCPProjection{access: access, drop: true}, nil
	}

	source, err := canonical.NewMCPSource(parsed.String(), allowed)
	if err != nil {
		return fail(err)
	}
	namespace, err := canonical.NewMCPToolNamespace(key, tool.ServerDescription, source, nil)
	if err != nil {
		return fail(err)
	}
	bearer := headerBearer
	if tool.Authorization != nil {
		bearer = *tool.Authorization
	}
	if bearer != "" {
		access, err = access.WithBearer(key, bearer)
		if err != nil {
			return fail(err)
		}
	}
	return responsesMCPProjection{declaration: namespace, access: access}, nil
}

func responsesMCPHeaderBearer(raw json.RawMessage) (string, bool, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "", false, nil
	}
	var headers map[string]string
	if err := json.Unmarshal(raw, &headers); err != nil {
		return "", false, canonical.BadRequest("responses MCP headers are invalid")
	}
	var bearer string
	custom := false
	for name, value := range headers {
		if !strings.EqualFold(strings.TrimSpace(name), "Authorization") {
			custom = true
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) < len("Bearer ") || !strings.EqualFold(value[:len("Bearer ")], "Bearer ") ||
			strings.TrimSpace(value[len("Bearer "):]) == "" {
			return "", false, canonical.BadRequest("responses MCP Authorization header must use Bearer authentication")
		}
		if bearer != "" {
			return "", false, canonical.BadRequest("responses MCP Authorization header is duplicated")
		}
		bearer = strings.TrimSpace(value[len("Bearer "):])
	}
	return bearer, custom, nil
}

func decodeResponsesToolOccurrences(tools []responsesToolDefinitionDTO, scope canonical.ContextScope, subjectPrefix string, sink compat.Sink, exchangeID string, access mcp.Access) ([]canonical.CanonicalItem, []canonical.ToolDeclaration, mcp.Access, error) {
	declarations := make([]canonical.ToolDeclaration, 0, len(tools))
	ordinary := make([]canonical.ToolDeclaration, 0, len(tools))
	for index, tool := range tools {
		if strings.EqualFold(strings.TrimSpace(tool.Type), "mcp") {
			projection, err := decodeResponsesMCPNamespace(tool, access)
			if err != nil {
				return nil, nil, access, err
			}
			if projection.drop {
				if err := emitResponsesCompatibilityDecision(
					sink,
					exchangeID,
					compat.RequestTools,
					compat.Drop,
					compat.Subject(fmt.Sprintf("%s/%d", subjectPrefix, index)),
				); err != nil {
					return nil, nil, access, err
				}
				continue
			}
			access = projection.access
			declarations = append(declarations, projection.declaration)
			continue
		}
		decoded, err := decodeResponsesToolNode(tool, responsesToolNamespaceContext{subjectPrefix: subjectPrefix, index: index}, compat.RequestToolsKind, sink, exchangeID)
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
