package connections

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"

	"github.com/swobuforge/swobu/internal/routing"
)

// InUseError carries exact consumers for the API's 409 projection. Error prose
// is deliberately constant; consumer identities are structured, never config.
type InUseError struct{ consumers []Consumer }

func (e *InUseError) Error() string         { return "Connection is referenced by Workspaces" }
func (e *InUseError) Consumers() []Consumer { return append([]Consumer(nil), e.consumers...) }

var ErrReferenceKind = errors.New("Workspace reference does not match Connection kind")

// Lifecycle coordinates routing and reusable identity publication. Both delete
// and reference admission acquire routing before Connection locks. No reverse
// index is persisted: consumers are derived from the locked current snapshot.
// API admission/idempotent receipts remain the application command boundary.
type Lifecycle struct {
	registry   *Store
	workspaces *configstore.Store
}

// References captures both the inspected Connection and its current consumers
// under routing-then-registry locks. Its revision binds advisory impact to the
// inspected configuration; callers cannot treat this result as delete consent.
func (l *Lifecycle) References(ctx context.Context, id connectionidentity.ID) (Connection, []Consumer, error) {
	var connection Connection
	var consumers []Consumer
	err := l.workspaces.InspectLocked(ctx, func(config routing.Config) error {
		l.registry.mu.RLock()
		defer l.registry.mu.RUnlock()
		value, ok := l.registry.values[id]
		if !ok {
			return ErrNotFound
		}
		connection = value
		consumers = References(config, value)
		return nil
	})
	if err != nil {
		return Connection{}, nil, err
	}
	return connection, consumers, nil
}

// NewLifecycle installs reference admission before commands can be served.
// Workspace references must resolve directly to an existing Connection of
// the required kind; there is no remaining source-registry fallback.
func NewLifecycle(registry *Store, workspaces *configstore.Store) (*Lifecycle, error) {
	if registry == nil || workspaces == nil {
		return nil, errors.New("Connection and Workspace stores are required")
	}
	lifecycle := &Lifecycle{registry: registry, workspaces: workspaces}
	if err := workspaces.InstallReferenceValidator(lifecycle.validateReferences); err != nil {
		return nil, err
	}
	return lifecycle, nil
}

func (l *Lifecycle) validateReferences(config routing.Config, reviewed *configstore.ReviewedConnection, commit func() error) error {
	// Routing lock is already held. Keep the registry immutable until the
	// candidate and its inline Connection section are durably published.
	l.registry.mu.RLock()
	defer l.registry.mu.RUnlock()
	// Preserve stale-review semantics even if the reviewed Connection was
	// deleted before this candidate reached reference admission.
	if reviewed != nil {
		id, direct := directReference(reviewed.ID)
		if !direct {
			return configstore.ErrMCPMountReviewConflict
		}
		value, exists := l.registry.values[id]
		if !exists || value.Spec().Kind() != MCP || value.Revision().String() != reviewed.Revision {
			return configstore.ErrMCPMountReviewConflict
		}
	}
	if err := validateRoutingReferences(config, l.registry.values); err != nil {
		return err
	}
	return commit()
}

// ValidateRoutingReferences performs the same typed admission as daemon startup
// without retaining a store, contacting providers or publishing a document.
func ValidateRoutingReferences(config routing.Config, connections []Connection) error {
	values := make(map[connectionidentity.ID]Connection, len(connections))
	for _, connection := range connections {
		if _, exists := values[connection.ID()]; exists {
			return ErrAlreadyExists
		}
		values[connection.ID()] = connection
	}
	return validateRoutingReferences(config, values)
}

func validateRoutingReferences(config routing.Config, values map[connectionidentity.ID]Connection) error {
	validate := func(path string, id connectionidentity.ID, kind Kind) error {
		connection, exists := values[id]
		if !exists {
			return fmt.Errorf("%s: %w (%s)", path, ErrNotFound, id.String())
		}
		if connection.Spec().Kind() != kind {
			return fmt.Errorf("%s: %w (%s is %s, expected %s)", path, ErrReferenceKind, id.String(), connection.Spec().Kind(), kind)
		}
		return nil
	}
	for _, workspace := range config.Workspaces() {
		prefix := "workspaces." + workspace.Slug().String()
		for _, route := range workspace.Routes() {
			for tierIndex, tier := range route.Tiers() {
				for targetIndex, target := range tier.Targets() {
					path := fmt.Sprintf("%s.routes.%s.tiers[%d].targets[%d].connection_id", prefix, route.Name().String(), tierIndex, targetIndex)
					if err := validate(path, target.ConnectionID(), Model); err != nil {
						return err
					}
					if err := ValidateModelTarget(target, values[target.ConnectionID()]); err != nil {
						return fmt.Errorf("%s: %w", path, err)
					}
				}
			}
		}
		for alias, mount := range workspace.MCPMounts() {
			if mount.Builtin == routing.AdminMCPSource && mount.ConnectionID == "" {
				continue
			}
			path := prefix + ".mcp_mounts." + alias + ".connection_id"
			id, direct := directReference(mount.ConnectionID)
			if !direct {
				return fmt.Errorf("%s: %w (expected a Connection ID)", path, ErrReferenceKind)
			}
			if err := validate(path, id, MCP); err != nil {
				return err
			}
		}
		for index, source := range workspace.SearchConnections() {
			path := prefix + ".search.connection_id"
			if index > 0 {
				path = fmt.Sprintf("%s.search.fallback_connection_ids[%d]", prefix, index-1)
			}
			id, direct := directReference(source)
			if !direct {
				return fmt.Errorf("%s: %w (expected a Connection ID)", path, ErrReferenceKind)
			}
			if err := validate(path, id, Search); err != nil {
				return err
			}
		}
	}
	return nil
}

// DeleteIfUnused requires the inspected revision and never cascades. Reference
// inspection and durable deletion share the routing publication boundary. A
// later routing commit must pass the installed validator before publication.
func (l *Lifecycle) DeleteIfUnused(ctx context.Context, id connectionidentity.ID, expected Revision) error {
	return l.deleteIfUnused(ctx, id, expected, "", nil)
}

// DeleteChecked retains a management fact atomically with reference-safe
// deletion. Admission repeats under routing-then-registry locks and must not
// reenter either store. Scope is trusted transport attribution. Missing/stale
// resources, referenced identities and failed writes never produce facts.
func (l *Lifecycle) DeleteChecked(ctx context.Context, id connectionidentity.ID, expected Revision, scope string, authorize func(context.Context) error) error {
	if scope == "" || authorize == nil {
		return errors.New("Connection deletion requires trusted scope and admission")
	}
	return l.deleteIfUnused(ctx, id, expected, scope, authorize)
}

func (l *Lifecycle) deleteIfUnused(ctx context.Context, id connectionidentity.ID, expected Revision, scope string, authorize func(context.Context) error) error {
	return l.workspaces.InspectLocked(ctx, func(config routing.Config) error {
		l.registry.mu.Lock()
		defer l.registry.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		if authorize != nil {
			if err := authorize(ctx); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		current, ok := l.registry.values[id]
		if !ok {
			return ErrNotFound
		}
		if expected.value == 0 || current.Revision() != expected {
			return ErrRevisionConflict
		}
		if consumers := References(config, current); len(consumers) != 0 {
			return &InUseError{consumers}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		values := make(map[connectionidentity.ID]Connection, len(l.registry.values)-1)
		for key, value := range l.registry.values {
			if key != id {
				values[key] = value
			}
		}
		audits := l.registry.audits
		if scope != "" {
			audits = appendAudit(audits, Audit{Action: "connection.delete", Caller: scope, ConnectionID: id.String(), BeforeRevision: current.Revision().String(), CommittedAt: time.Now().UTC()})
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := l.registry.publishDocument(values, l.registry.creations, audits); err != nil {
			return err
		}
		delete(l.registry.checks, id)
		return nil
	})
}
