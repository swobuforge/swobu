package canonical

import "strings"

type MCPSourceKind string

const (
	MCPSourceURL         MCPSourceKind = "url"
	MCPSourceConnectorID MCPSourceKind = "connector_id"
	MCPSourceTunnelID    MCPSourceKind = "tunnel_id"
)

type MCPLoading string

const (
	MCPLoadingEager    MCPLoading = "eager"
	MCPLoadingDeferred MCPLoading = "deferred"
)

type MCPApprovalKind string

const (
	MCPApprovalNever  MCPApprovalKind = "never"
	MCPApprovalAlways MCPApprovalKind = "always"
	MCPApprovalFilter MCPApprovalKind = "filter"
)

// MCPToolFilter is one typed selector inside an approval policy.
type MCPToolFilter struct {
	toolNames Specified[[]string]
	readOnly  Specified[bool]
}

func NewMCPToolFilter(toolNames Specified[[]string], readOnly Specified[bool]) (MCPToolFilter, error) {
	names, err := normalizeMCPNames(toolNames, "canonical MCP approval tool")
	if err != nil {
		return MCPToolFilter{}, err
	}
	return MCPToolFilter{toolNames: names, readOnly: readOnly}, nil
}

func (f MCPToolFilter) ToolNames() Specified[[]string] {
	return cloneSpecifiedStrings(f.toolNames)
}

func (f MCPToolFilter) ReadOnly() Specified[bool] { return f.readOnly }

func (f MCPToolFilter) clone() MCPToolFilter {
	return MCPToolFilter{toolNames: f.ToolNames(), readOnly: f.readOnly}
}

func (f MCPToolFilter) equivalent(other MCPToolFilter) bool {
	return specifiedStringsEqual(f.toolNames, other.toolNames) && f.readOnly == other.readOnly
}

// MCPApproval is a closed approval policy. Filter policies retain both
// precedence branches because weakening either branch can broaden authority.
type MCPApproval struct {
	kind   MCPApprovalKind
	always *MCPToolFilter
	never  *MCPToolFilter
}

func NewMCPApprovalNever() MCPApproval  { return MCPApproval{kind: MCPApprovalNever} }
func NewMCPApprovalAlways() MCPApproval { return MCPApproval{kind: MCPApprovalAlways} }

func NewMCPApprovalFilter(always, never *MCPToolFilter) (MCPApproval, error) {
	if always == nil && never == nil {
		return MCPApproval{}, BadRequest("canonical MCP approval policy is empty")
	}
	approval := MCPApproval{kind: MCPApprovalFilter}
	if always != nil {
		value := always.clone()
		approval.always = &value
	}
	if never != nil {
		value := never.clone()
		approval.never = &value
	}
	return approval, nil
}

func (a MCPApproval) Kind() MCPApprovalKind { return a.kind }

func (a MCPApproval) AlwaysFilter() (MCPToolFilter, bool) {
	if a.always == nil {
		return MCPToolFilter{}, false
	}
	return a.always.clone(), true
}

func (a MCPApproval) NeverFilter() (MCPToolFilter, bool) {
	if a.never == nil {
		return MCPToolFilter{}, false
	}
	return a.never.clone(), true
}

func (a MCPApproval) clone() MCPApproval {
	switch a.kind {
	case MCPApprovalNever:
		return NewMCPApprovalNever()
	case MCPApprovalAlways:
		return NewMCPApprovalAlways()
	case MCPApprovalFilter:
		cloned, _ := NewMCPApprovalFilter(a.always, a.never)
		return cloned
	default:
		return MCPApproval{}
	}
}

func (a MCPApproval) equivalent(other MCPApproval) bool {
	if a.kind != other.kind {
		return false
	}
	for _, pair := range [][2]*MCPToolFilter{{a.always, other.always}, {a.never, other.never}} {
		if (pair[0] == nil) != (pair[1] == nil) {
			return false
		}
		if pair[0] != nil && !pair[0].equivalent(*pair[1]) {
			return false
		}
	}
	return true
}

// MCPSource refines one namespace with known request-carried discovery,
// approval, loading, and caller-authority semantics. Credentials and headers
// remain in request-private mcp.Access, never in canonical history.
type MCPSource struct {
	kind           MCPSourceKind
	reference      string
	allowedTools   Specified[[]string]
	approval       MCPApproval
	loading        MCPLoading
	allowedCallers Specified[[]string]
}

func NewMCPURLSource(endpoint string, allowedTools Specified[[]string], approval MCPApproval, loading MCPLoading, allowedCallers Specified[[]string]) (MCPSource, error) {
	return newMCPSource(MCPSourceURL, endpoint, allowedTools, approval, loading, allowedCallers)
}

func NewMCPConnectorSource(connectorID string, allowedTools Specified[[]string], approval MCPApproval, loading MCPLoading, allowedCallers Specified[[]string]) (MCPSource, error) {
	return newMCPSource(MCPSourceConnectorID, connectorID, allowedTools, approval, loading, allowedCallers)
}

func NewMCPTunnelSource(tunnelID string, allowedTools Specified[[]string], approval MCPApproval, loading MCPLoading, allowedCallers Specified[[]string]) (MCPSource, error) {
	return newMCPSource(MCPSourceTunnelID, tunnelID, allowedTools, approval, loading, allowedCallers)
}

func newMCPSource(kind MCPSourceKind, reference string, allowedTools Specified[[]string], approval MCPApproval, loading MCPLoading, allowedCallers Specified[[]string]) (MCPSource, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return MCPSource{}, BadRequest("canonical remote MCP source is invalid")
	}
	switch kind {
	case MCPSourceURL, MCPSourceConnectorID, MCPSourceTunnelID:
	default:
		return MCPSource{}, BadRequest("canonical MCP source kind is invalid")
	}
	allowed, err := normalizeMCPNames(allowedTools, "canonical MCP allowed tool")
	if err != nil {
		return MCPSource{}, err
	}
	callers, err := normalizeMCPNames(allowedCallers, "canonical MCP allowed caller")
	if err != nil {
		return MCPSource{}, err
	}
	switch approval.Kind() {
	case MCPApprovalNever, MCPApprovalAlways, MCPApprovalFilter:
	default:
		return MCPSource{}, BadRequest("canonical MCP approval policy is invalid")
	}
	if loading == "" {
		loading = MCPLoadingEager
	}
	if loading != MCPLoadingEager && loading != MCPLoadingDeferred {
		return MCPSource{}, BadRequest("canonical MCP loading policy is invalid")
	}
	return MCPSource{
		kind: kind, reference: reference, allowedTools: allowed,
		approval: approval.clone(), loading: loading, allowedCallers: callers,
	}, nil
}

func (s MCPSource) Kind() MCPSourceKind { return s.kind }

func (s MCPSource) URL() (string, bool) {
	return s.reference, s.kind == MCPSourceURL
}

func (s MCPSource) ConnectorID() (string, bool) {
	return s.reference, s.kind == MCPSourceConnectorID
}

func (s MCPSource) TunnelID() (string, bool) {
	return s.reference, s.kind == MCPSourceTunnelID
}

func (s MCPSource) AllowedTools() Specified[[]string] {
	return cloneSpecifiedStrings(s.allowedTools)
}

func (s MCPSource) Approval() MCPApproval { return s.approval.clone() }
func (s MCPSource) Loading() MCPLoading   { return s.loading }

func (s MCPSource) AllowedCallers() Specified[[]string] {
	return cloneSpecifiedStrings(s.allowedCallers)
}

func (s MCPSource) Clone() MCPSource {
	return MCPSource{
		kind: s.kind, reference: s.reference, allowedTools: s.AllowedTools(),
		approval: s.approval.clone(), loading: s.loading,
		allowedCallers: s.AllowedCallers(),
	}
}

func (s MCPSource) Equivalent(other MCPSource) bool {
	return s.kind == other.kind && s.reference == other.reference &&
		specifiedStringsEqual(s.allowedTools, other.allowedTools) &&
		s.approval.equivalent(other.approval) && s.loading == other.loading &&
		specifiedStringsEqual(s.allowedCallers, other.allowedCallers)
}

func normalizeMCPNames(values Specified[[]string], subject string) (Specified[[]string], error) {
	raw, ok := values.Get()
	if !ok {
		return Unspecified[[]string](), nil
	}
	seen := make(map[string]struct{}, len(raw))
	normalized := make([]string, len(raw))
	for index, value := range raw {
		value = strings.TrimSpace(value)
		if value == "" {
			return Unspecified[[]string](), BadRequest(subject + " name is empty")
		}
		if _, duplicate := seen[value]; duplicate {
			return Unspecified[[]string](), BadRequest(subject + " names contain a duplicate")
		}
		seen[value] = struct{}{}
		normalized[index] = value
	}
	return Specify(normalized), nil
}

func cloneSpecifiedStrings(values Specified[[]string]) Specified[[]string] {
	raw, ok := values.Get()
	if !ok {
		return Unspecified[[]string]()
	}
	return Specify(append([]string(nil), raw...))
}

func specifiedStringsEqual(left, right Specified[[]string]) bool {
	leftValues, leftSet := left.Get()
	rightValues, rightSet := right.Get()
	if leftSet != rightSet || len(leftValues) != len(rightValues) {
		return false
	}
	for index := range leftValues {
		if leftValues[index] != rightValues[index] {
			return false
		}
	}
	return true
}
