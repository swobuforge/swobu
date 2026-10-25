package accessstate

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/routing"
)

// Nil Services means the explicit Workspace-wide grant supported by existing
// installations. A non-empty set is narrower and never gains newly introduced
// Services. Empty non-nil sets, unknown values, and duplicates fail closed.
func canonicalServices(services []routing.ServiceKind) ([]routing.ServiceKind, error) {
	if services == nil {
		return nil, nil
	}
	if len(services) == 0 {
		return nil, errors.New("permission must name at least one Service")
	}
	seen := make(map[routing.ServiceKind]bool, len(services))
	for _, kind := range services {
		if _, err := routing.ParseServiceKind(string(kind)); err != nil {
			return nil, fmt.Errorf("permission Services: %w", err)
		}
		if seen[kind] {
			return nil, fmt.Errorf("duplicate permission Service %q", kind)
		}
		seen[kind] = true
	}
	out := make([]routing.ServiceKind, 0, len(seen))
	for _, kind := range []routing.ServiceKind{routing.ServiceModels, routing.ServiceMCP, routing.ServiceSearch} {
		if seen[kind] {
			out = append(out, kind)
		}
	}
	return out, nil
}

func scopeFingerprint(services []routing.ServiceKind) string {
	if services == nil {
		return ""
	}
	parts := make([]string, len(services))
	for i, kind := range services {
		parts[i] = string(kind)
	}
	return "\x00services:" + strings.Join(parts, ",")
}

func permissionAllowsService(access Access, workspace routing.WorkspaceSlug, service routing.ServiceKind, now time.Time) bool {
	if !permissionAllows(access, workspace, now) {
		return false
	}
	for _, p := range access.Permissions {
		if p.Workspace != workspace {
			continue
		}
		if p.Services == nil {
			return true
		}
		for _, allowed := range p.Services {
			if allowed == service {
				return true
			}
		}
		return false
	}
	return false
}

// AllowsService projects grant impact for a reviewed snapshot. This is advisory;
// ingress must resolve the live credential again before any actual effect.
func (access Access) AllowsService(workspace routing.WorkspaceSlug, service routing.ServiceKind, at time.Time) bool {
	return permissionAllowsService(access, workspace, service, at)
}
