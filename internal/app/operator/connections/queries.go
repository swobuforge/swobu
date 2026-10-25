package connections

import (
	"context"
	"errors"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"

	domain "github.com/swobuforge/swobu/internal/connections"
)

// Queries projects only operator-safe values. It does not own network checks or
// authorization; the transport must admit the caller before invoking it.
type Queries struct {
	registry  *domain.Store
	lifecycle *domain.Lifecycle
}

func NewQueries(registry *domain.Store, lifecycle *domain.Lifecycle) (*Queries, error) {
	if registry == nil || lifecycle == nil {
		return nil, errors.New("Connection registry and lifecycle are required")
	}
	return &Queries{registry, lifecycle}, nil
}

type Filter struct {
	Kind  domain.Kind
	Query string
}

func (q *Queries) List(ctx context.Context, filter Filter) ([]Connection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if filter.Kind != "" && filter.Kind != domain.Model && filter.Kind != domain.MCP && filter.Kind != domain.Search {
		return nil, errors.New("invalid Connection kind filter")
	}
	needle := strings.ToLower(strings.TrimSpace(filter.Query))
	result := make([]Connection, 0)
	for _, snapshot := range q.registry.Snapshots() {
		if filter.Kind != "" && snapshot.Connection.Spec().Kind() != filter.Kind {
			continue
		}
		value, err := Project(snapshot.Connection, snapshot.LatestCheck)
		if err != nil {
			return nil, err
		}
		// Search only public display values. Credential paths, stdio argv and
		// stripped URL tokens must not become an indirect disclosure oracle.
		if needle != "" && !strings.Contains(strings.ToLower(value.DisplayName), needle) && !strings.Contains(strings.ToLower(endpointOf(value.Spec)), needle) {
			continue
		}
		result = append(result, value)
	}
	return result, nil
}

func endpointOf(spec Spec) string {
	switch spec := spec.(type) {
	case StandardModel:
		return spec.Endpoint
	case BedrockModel:
		return spec.Endpoint
	case CustomModel:
		return spec.Endpoint
	case MCPHTTP:
		return spec.Endpoint
	case Search:
		return spec.Endpoint
	default:
		return ""
	}
}

func (q *Queries) Get(ctx context.Context, id connectionidentity.ID) (Connection, error) {
	if err := ctx.Err(); err != nil {
		return Connection{}, err
	}
	snapshot, err := q.registry.Snapshot(id)
	if err != nil {
		return Connection{}, err
	}
	return Project(snapshot.Connection, snapshot.LatestCheck)
}

type Reference struct {
	Workspace string      `json:"workspace"`
	Kind      domain.Kind `json:"kind"`
	Route     string      `json:"route,omitempty"`
	Target    string      `json:"target,omitempty"`
	Mount     string      `json:"mount,omitempty"`
}

type References struct {
	ConnectionID       string      `json:"connection_id"`
	ConnectionRevision string      `json:"connection_revision"`
	Consumers          []Reference `json:"consumers"`
}

func (q *Queries) References(ctx context.Context, id connectionidentity.ID) (References, error) {
	connection, consumers, err := q.lifecycle.References(ctx, id)
	if err != nil {
		return References{}, err
	}
	result := References{connection.ID().String(), connection.Revision().String(), make([]Reference, 0, len(consumers))}
	for _, consumer := range consumers {
		result.Consumers = append(result.Consumers, Reference{consumer.Workspace, consumer.Kind, consumer.Route, consumer.Target, consumer.Mount})
	}
	return result, nil
}
