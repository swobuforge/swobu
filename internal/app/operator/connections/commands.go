package connections

import (
	"context"
	"errors"
	"time"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"

	domain "github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

// Commands is the shared application owner for reusable Connection mutations.
// Admission must come from the trusted transport and is rechecked at effect
// time. It must not call back into the Connection registry. Requests carry
// validated private specs; results expose only the redacted public read model.
type Commands struct {
	registry  *domain.Store
	lifecycle *domain.Lifecycle
}

func NewCommands(registry *domain.Store, lifecycle *domain.Lifecycle) (*Commands, error) {
	if registry == nil || lifecycle == nil {
		return nil, errors.New("Connection registry and lifecycle are required")
	}
	return &Commands{registry, lifecycle}, nil
}

func (c *Commands) Create(ctx context.Context, label string, spec domain.Spec, scope, key string, authorize func(context.Context) error) (Connection, bool, error) {
	value, replayed, err := c.registry.CreateChecked(ctx, label, spec, scope, key, authorize)
	if err != nil {
		return Connection{}, false, err
	}
	result, err := Project(value, nil)
	return result, replayed, err
}

var ErrIdentityAssertion = errors.New("confirm the Connection still represents the same external account or server")

// EditSameIdentity requires explicit operator assertion because endpoint or
// credential syntax cannot establish external identity. The store revalidates
// the revision during publication, so a concurrent editor cannot be overwritten.
// Trusted scope is retained in the atomic management fact, never inferred from
// the operator-controlled label or spec.
func (c *Commands) EditSameIdentity(ctx context.Context, id connectionidentity.ID, expected domain.Revision, label string, spec domain.Spec, sameIdentity bool, scope string, authorize func(context.Context) error) (Connection, error) {
	if !sameIdentity {
		return Connection{}, ErrIdentityAssertion
	}
	if authorize == nil {
		return Connection{}, errors.New("Connection edit requires admission")
	}
	if err := ctx.Err(); err != nil {
		return Connection{}, err
	}
	current, err := c.registry.Get(id)
	if err != nil {
		return Connection{}, err
	}
	candidate, err := current.EditSameIdentity(expected, label, spec, time.Now().UTC())
	if err != nil {
		return Connection{}, err
	}
	// PublishChecked repeats admission under the registry's publication lock.
	if err := c.registry.PublishChecked(ctx, candidate, expected, scope, authorize); err != nil {
		return Connection{}, err
	}
	return Project(candidate, nil)
}

// Delete refuses referenced identities and never cascades. Revision review and
// trusted admission are rechecked at the shared Workspace/Connection boundary;
// durable removal and its management fact commit together.
func (c *Commands) Delete(ctx context.Context, id connectionidentity.ID, expected domain.Revision, scope string, authorize func(context.Context) error) error {
	return c.lifecycle.DeleteChecked(ctx, id, expected, scope, authorize)
}

// ReplaceCredentialBinding publishes a previously staged immutable credential
// locator. The caller must assert unchanged external identity; endpoint/provider
// changes are not accepted. This is the shared binding command, not a raw-secret
// storage or retirement API. Trusted scope and key bind durable replay while
// responses remain redacted. Staging/cleanup belongs to the material owner.
func (c *Commands) ReplaceCredentialBinding(ctx context.Context, id connectionidentity.ID, expected domain.Revision, environment string, old, replacement credentialref.Ref, sameIdentity bool, scope, key string, authorize func(context.Context) error) (Connection, bool, error) {
	if !sameIdentity {
		return Connection{}, false, ErrIdentityAssertion
	}
	value, replayed, err := c.registry.ReplaceCredentialChecked(ctx, id, expected, environment, old, replacement, scope, key, authorize)
	if err != nil {
		return Connection{}, false, err
	}
	result, err := Project(value, nil)
	return result, replayed, err
}
