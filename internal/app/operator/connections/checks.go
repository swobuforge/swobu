package connections

import (
	"context"
	"errors"
	"time"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"

	domain "github.com/swobuforge/swobu/internal/connections"
)

// CheckTimeout reuses the existing MCP discovery deployment envelope. It bounds
// the complete operator probe, including credential resolution and discovery.
// Adapters must honor cancellation and retain protocol-specific response limits.
const CheckTimeout = 15 * time.Second

// Probe returns only validated, sanitized observations for its exact snapshot.
// Provider bodies, command output, credentials and locators never enter errors
// or readback. The adapter translates failures into closed observation codes.
type Probe func(context.Context, domain.Connection) (domain.Check, error)

func (c *Commands) Check(ctx context.Context, id connectionidentity.ID, expected domain.Revision, authorize func(context.Context) error, probe Probe) (Check, error) {
	if probe == nil {
		return Check{}, errors.New("Connection probe required")
	}
	value, err := c.registry.InspectCheckChecked(ctx, id, expected, authorize)
	if err != nil {
		return Check{}, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, CheckTimeout)
	defer cancel()
	observation, err := probe(probeCtx, value)
	if errors.Is(probeCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
		observation, err = domain.NewCheck(value, time.Now().UTC(), domain.ReachabilityUnknown, domain.AuthenticationUnknown, nil, domain.CheckTimeout)
	}
	if err != nil {
		return Check{}, errors.New("Connection probe failed to produce an observation")
	}
	if observation.ConnectionID() != id || observation.ConnectionRevision() != expected {
		return Check{}, errors.New("Connection probe returned a different identity or revision")
	}
	// A canceled caller cannot publish; a probe deadline may still produce a
	// truthful timeout observation while the requesting caller remains admitted.
	if err := c.registry.ObserveChecked(ctx, observation, authorize); err != nil {
		return Check{}, err
	}
	projected, err := Project(value, &observation)
	if err != nil {
		return Check{}, err
	}
	if projected.LatestCheck == nil {
		return Check{}, errors.New("Connection observation is invalid")
	}
	return *projected.LatestCheck, nil
}
