package connections

import (
	"sort"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

func directReference(raw string) (connectionidentity.ID, bool) {
	id, err := connectionidentity.Parse(raw)
	return id, err == nil
}

// Consumer names one Workspace child without exposing upstream configuration
// or credential locators. Route and Target are populated for Models, Mount for
// MCP, and neither for the Workspace Search Connection.
type Consumer struct {
	Workspace string
	Kind      Kind
	Route     string
	Target    string
	Mount     string
}

// References projects direct Model, MCP, and Search references. This pure
// projection is not deletion authorization: callers must
// retain the Workspace publication lock throughout inspection and deletion.
func References(config routing.Config, connection Connection) []Consumer {
	consumers := make([]Consumer, 0)
	for _, workspace := range config.Workspaces() {
		slug := workspace.Slug().String()
		switch connection.Spec().(type) {
		case ModelSpec:
			for _, route := range workspace.Routes() {
				for _, tier := range route.Tiers() {
					for _, target := range tier.Targets() {
						if target.ConnectionID() == connection.ID() {
							consumers = append(consumers, Consumer{Workspace: slug, Kind: Model, Route: route.Name().String(), Target: target.ID().String()})
						}

					}
				}
			}
		case MCPHTTPSpec, MCPStdioSpec:
			for alias, mount := range workspace.MCPMounts() {
				if id, ok := directReference(mount.ConnectionID); ok && id == connection.ID() {
					consumers = append(consumers, Consumer{Workspace: slug, Kind: MCP, Mount: alias})
				}
			}
		case SearchSpec:
			for _, source := range workspace.SearchConnections() {
				if id, ok := directReference(source); ok && id == connection.ID() {
					consumers = append(consumers, Consumer{Workspace: slug, Kind: Search})
				}
			}
		}
	}
	sort.Slice(consumers, func(i, j int) bool {
		left, right := consumers[i], consumers[j]
		if left.Workspace != right.Workspace {
			return left.Workspace < right.Workspace
		}
		if left.Route != right.Route {
			return left.Route < right.Route
		}
		if left.Target != right.Target {
			return left.Target < right.Target
		}
		return left.Mount < right.Mount
	})
	return consumers
}
