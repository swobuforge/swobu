package canonical

import "strings"

// MCPSource refines one namespace with request-carried remote discovery
// configuration. Namespace identity, description, scope, and resolved children
// remain owned by the enclosing declaration occurrence.
type MCPSource struct {
	endpoint     string
	allowedTools Specified[[]string]
}

func NewMCPSource(endpoint string, allowedTools Specified[[]string]) (MCPSource, error) {
	if strings.TrimSpace(endpoint) == "" {
		return MCPSource{}, BadRequest("canonical remote MCP source is invalid")
	}
	normalized := Unspecified[[]string]()
	if values, ok := allowedTools.Get(); ok {
		seen := map[string]struct{}{}
		copyValues := make([]string, len(values))
		for index, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				return MCPSource{}, BadRequest("canonical MCP allowed tool name is empty")
			}
			if _, duplicate := seen[value]; duplicate {
				return MCPSource{}, BadRequest("canonical MCP allowed tools contain a duplicate")
			}
			seen[value] = struct{}{}
			copyValues[index] = value
		}
		normalized = Specify(copyValues)
	}
	return MCPSource{endpoint: strings.TrimSpace(endpoint), allowedTools: normalized}, nil
}

func (s MCPSource) Endpoint() string { return s.endpoint }
func (s MCPSource) AllowedTools() Specified[[]string] {
	values, ok := s.allowedTools.Get()
	if !ok {
		return Unspecified[[]string]()
	}
	return Specify(append([]string(nil), values...))
}
func (s MCPSource) Clone() MCPSource {
	return MCPSource{endpoint: s.endpoint, allowedTools: s.AllowedTools()}
}
func (s MCPSource) Equivalent(other MCPSource) bool {
	if s.endpoint != other.endpoint {
		return false
	}
	left, leftSpecified := s.allowedTools.Get()
	right, rightSpecified := other.allowedTools.Get()
	if leftSpecified != rightSpecified || len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
