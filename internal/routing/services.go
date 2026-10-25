package routing

import (
	"errors"
	"fmt"
)

// ServiceKind is one directly addressable Workspace capability in the current release.
// Decisions and Images are not implemented Services and are intentionally excluded.
type ServiceKind string

const (
	ServiceModels ServiceKind = "models"
	ServiceMCP    ServiceKind = "mcp"
	ServiceSearch ServiceKind = "search"
)

var ErrServiceDisabled = errors.New("workspace service is disabled")
var ErrServiceNotEmpty = errors.New("workspace service has configuration")

func ParseServiceKind(raw string) (ServiceKind, error) {
	kind := ServiceKind(raw)
	switch kind {
	case ServiceModels, ServiceMCP, ServiceSearch:
		return kind, nil
	default:
		return "", fmt.Errorf("invalid Workspace service %q (expected models, mcp or search)", raw)
	}
}

// EnabledServices returns the explicitly enabled set in deterministic order.
func (w Workspace) EnabledServices() []ServiceKind {
	result := make([]ServiceKind, 0, len(w.enabledServices))
	for _, kind := range []ServiceKind{ServiceModels, ServiceMCP, ServiceSearch} {
		if w.HasService(kind) {
			result = append(result, kind)
		}
	}
	return result
}

func (w Workspace) HasService(kind ServiceKind) bool { _, ok := w.enabledServices[kind]; return ok }

// WithServices restores the declared set, including an intentionally empty set.
// A Service may be enabled with no configuration but cannot be disabled while
// it still owns route, mount or Search configuration.
func (w Workspace) WithServices(kinds []ServiceKind) (Workspace, error) {
	w = w.clone()
	w.enabledServices = make(map[ServiceKind]struct{}, len(kinds))
	for _, kind := range kinds {
		if _, err := ParseServiceKind(string(kind)); err != nil {
			return Workspace{}, err
		}
		if _, exists := w.enabledServices[kind]; exists {
			return Workspace{}, fmt.Errorf("enabled_services: duplicate %q", kind)
		}
		w.enabledServices[kind] = struct{}{}
	}
	if err := w.validate(); err != nil {
		return Workspace{}, err
	}
	return w, nil
}

func (w Workspace) SetServiceEnabled(kind ServiceKind, enabled bool) (Workspace, error) {
	if _, err := ParseServiceKind(string(kind)); err != nil {
		return Workspace{}, err
	}
	w = w.clone()
	if enabled {
		w.enabledServices[kind] = struct{}{}
	} else {
		delete(w.enabledServices, kind)
	}
	if err := w.validate(); err != nil {
		return Workspace{}, err
	}
	return w, nil
}

func (c Config) SetServiceEnabled(slug WorkspaceSlug, kind ServiceKind, enabled bool) (Config, error) {
	return c.editWorkspace(slug, func(w Workspace) (Workspace, error) {
		return w.SetServiceEnabled(kind, enabled)
	})
}
