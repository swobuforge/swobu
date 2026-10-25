package routing

// ServiceQuota is an operator-configured Workspace-wide admission ceiling.
// Zero means unlimited. This is not a spend limit or a Principal grant.
// Concurrent is currently meaningful only for Models.
type ServiceQuota struct {
	RequestsPerMinute uint32 `json:"requests_per_minute" yaml:"requests_per_minute"`
	Concurrent        uint32 `json:"concurrent,omitempty" yaml:"concurrent,omitempty"`
}

// ServiceLimits limits downstream operations, regardless of client, Principal,
// or number of upstream attempts. It is a value on the existing Workspace.
type ServiceLimits struct {
	Models ServiceQuota `json:"models" yaml:"models"`
	MCP    ServiceQuota `json:"mcp" yaml:"mcp"`
	Search ServiceQuota `json:"search" yaml:"search"`
}

// Validate enforces product configuration bounds (100000 RPM, 10000 Models
// slots), not provider protocol limits or a guarantee of Node capacity.
func (p ServiceLimits) Validate() error {
	for _, entry := range []struct {
		kind  ServiceKind
		value ServiceQuota
	}{
		{ServiceModels, p.Models}, {ServiceMCP, p.MCP}, {ServiceSearch, p.Search},
	} {
		if entry.value.RequestsPerMinute > 100000 {
			return pathError("service_limits."+string(entry.kind)+".requests_per_minute", "must not exceed 100000")
		}
		if entry.kind != ServiceModels && entry.value.Concurrent != 0 {
			return pathError("service_limits."+string(entry.kind)+".concurrent", "only Models supports concurrent admission")
		}
	}
	if p.Models.Concurrent > 10000 {
		return pathError("service_limits.models.concurrent", "must not exceed 10000")
	}
	return nil
}

func (p ServiceLimits) For(kind ServiceKind) ServiceQuota {
	switch kind {
	case ServiceModels:
		return p.Models
	case ServiceMCP:
		return p.MCP
	case ServiceSearch:
		return p.Search
	default:
		return ServiceQuota{}
	}
}

func (w Workspace) ServiceLimits() ServiceLimits { return w.serviceLimits }

func (w Workspace) WithServiceLimits(p ServiceLimits) (Workspace, error) {
	if err := p.Validate(); err != nil {
		return Workspace{}, err
	}
	w = w.clone()
	w.serviceLimits = p
	return w, nil
}

func (c Config) SetServiceLimits(slug WorkspaceSlug, p ServiceLimits) (Config, error) {
	return c.editWorkspace(slug, func(w Workspace) (Workspace, error) { return w.WithServiceLimits(p) })
}
