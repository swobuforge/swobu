package connections

import (
	"context"
	"errors"
	"time"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
)

// Reachability and Authentication describe only what a check observed. Unknown
// is explicit: opening a socket does not establish successful authentication.
type Reachability string
type Authentication string

const (
	ReachabilityUnknown       Reachability   = "unknown"
	Reachable                 Reachability   = "reachable"
	Unreachable               Reachability   = "unreachable"
	AuthenticationUnknown     Authentication = "unknown"
	AuthenticationAccepted    Authentication = "accepted"
	AuthenticationRejected    Authentication = "rejected"
	AuthenticationNotRequired Authentication = "not_required"
)

// CheckError is a closed public projection. Probe adapters translate errors at
// their edge; upstream bodies, executable output and credential values must
// never become health error prose.
type CheckError string

const (
	CheckOK                    CheckError = ""
	CheckTimeout               CheckError = "timeout"
	CheckNetworkDenied         CheckError = "network_denied"
	CheckUnavailable           CheckError = "unavailable"
	CheckAuthenticationFailed  CheckError = "authentication_failed"
	CheckInvalidResponse       CheckError = "invalid_response"
	CheckCredentialUnavailable CheckError = "credential_unavailable"
)

// Capability is a successfully observed discovery surface, not a prediction of
// request compatibility. Absent capabilities remain unknown.
type Capability string

const (
	CapabilityModels Capability = "models"
	CapabilityTools  Capability = "tools"
	CapabilitySearch Capability = "search"
)

// Check is immutable and tied to the exact configuration inspected. It is
// ephemeral evidence, excluded from the durable configuration document.
type Check struct {
	id             connectionidentity.ID
	revision       Revision
	checkedAt      time.Time
	reachability   Reachability
	authentication Authentication
	capabilities   []Capability
	failure        CheckError
}

func NewCheck(connection Connection, checkedAt time.Time, reachability Reachability, authentication Authentication, capabilities []Capability, failure CheckError) (Check, error) {
	if connection.id.String() == "" || connection.revision.value == 0 || checkedAt.IsZero() || checkedAt.Before(connection.updatedAt) {
		return Check{}, errors.New("invalid Connection check identity or time")
	}
	if reachability != ReachabilityUnknown && reachability != Reachable && reachability != Unreachable {
		return Check{}, errors.New("invalid Connection reachability")
	}
	if authentication != AuthenticationUnknown && authentication != AuthenticationAccepted && authentication != AuthenticationRejected && authentication != AuthenticationNotRequired {
		return Check{}, errors.New("invalid Connection authentication observation")
	}
	switch failure {
	case CheckOK, CheckTimeout, CheckNetworkDenied, CheckUnavailable, CheckAuthenticationFailed, CheckInvalidResponse, CheckCredentialUnavailable:
	default:
		return Check{}, errors.New("invalid Connection check error code")
	}
	if reachability != Reachable && (authentication != AuthenticationUnknown || len(capabilities) != 0) {
		return Check{}, errors.New("Connection check requires reachability before authentication or discovery")
	}
	if authentication == AuthenticationRejected && len(capabilities) != 0 {
		return Check{}, errors.New("Connection discovery cannot succeed with rejected authentication")
	}
	seen := make(map[Capability]bool, len(capabilities))
	for _, capability := range capabilities {
		if capability != CapabilityModels && capability != CapabilityTools && capability != CapabilitySearch || seen[capability] {
			return Check{}, errors.New("invalid Connection capability observation")
		}
		if capability == CapabilityModels && connection.spec.Kind() != Model || capability == CapabilityTools && connection.spec.Kind() != MCP || capability == CapabilitySearch && connection.spec.Kind() != Search {
			return Check{}, errors.New("Connection capability kind mismatch")
		}
		seen[capability] = true
	}
	return Check{connection.id, connection.revision, checkedAt.UTC(), reachability, authentication, append([]Capability(nil), capabilities...), failure}, nil
}

func (c Check) ConnectionID() connectionidentity.ID { return c.id }
func (c Check) ConnectionRevision() Revision        { return c.revision }
func (c Check) CheckedAt() time.Time                { return c.checkedAt }
func (c Check) Reachability() Reachability          { return c.reachability }
func (c Check) Authentication() Authentication      { return c.authentication }
func (c Check) Capabilities() []Capability          { return append([]Capability(nil), c.capabilities...) }
func (c Check) ErrorCode() CheckError               { return c.failure }

// Observe rejects late completions for edited configurations. Concurrent checks
// may finish out of order; only the most recent observation remains projected.
// No disk write or configuration revision change occurs.
func (s *Store) Observe(check Check) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.observeLocked(check)
}

func (s *Store) observeLocked(check Check) error {
	current, ok := s.values[check.id]
	if !ok {
		return ErrNotFound
	}
	if current.revision != check.revision {
		return ErrRevisionConflict
	}
	if _, err := NewCheck(current, check.checkedAt, check.reachability, check.authentication, check.capabilities, check.failure); err != nil {
		return err
	}
	if s.checks == nil {
		s.checks = make(map[connectionidentity.ID]Check)
	}
	if previous, ok := s.checks[check.id]; ok && previous.revision == check.revision && !check.checkedAt.After(previous.checkedAt) {
		return nil
	}
	s.checks[check.id] = check
	return nil
}

// LatestCheck returns evidence only for the current revision. Callers must
// display CheckedAt; even current-revision success is not perpetual readiness.
func (s *Store) LatestCheck(id connectionidentity.ID) (Check, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	current, exists := s.values[id]
	check, observed := s.checks[id]
	if !exists || !observed || check.revision != current.revision {
		return Check{}, false
	}
	return check, true
}

// InspectCheckChecked captures exactly the revision the operator reviewed.
// Admission runs under the registry lock but the caller's network probe must run
// after return. Admission callbacks cannot reenter the registry.
func (s *Store) InspectCheckChecked(ctx context.Context, id connectionidentity.ID, expected Revision, authorize func(context.Context) error) (Connection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if authorize == nil {
		return Connection{}, errors.New("Connection check requires admission")
	}
	if err := ctx.Err(); err != nil {
		return Connection{}, err
	}
	if err := authorize(ctx); err != nil {
		return Connection{}, err
	}
	if err := ctx.Err(); err != nil {
		return Connection{}, err
	}
	value, exists := s.values[id]
	if !exists {
		return Connection{}, ErrNotFound
	}
	if expected.value == 0 || value.Revision() != expected {
		return Connection{}, ErrRevisionConflict
	}
	return value, nil
}

// ObserveChecked repeats live authority at the observation publication boundary.
// This is ephemeral evidence, not an attributed management/configuration change.
func (s *Store) ObserveChecked(ctx context.Context, check Check, authorize func(context.Context) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if authorize == nil {
		return errors.New("Connection check requires admission")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := authorize(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.observeLocked(check)
}
