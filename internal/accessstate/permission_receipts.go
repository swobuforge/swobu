package accessstate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

var ErrPermissionIdempotencyConflict = errors.New("idempotency key conflicts with a different permission mutation")

// PermissionReceipt is transaction metadata, not a Grant identity. Only hashes
// are persisted: neither retry keys nor credential material enter this record.
// Caller is trusted local Node management authority, never the target Principal.
type PermissionReceipt struct {
	Caller     string           `json:"caller,omitempty"`
	KeyHash    string           `json:"key_hash"`
	IntentHash string           `json:"intent_hash"`
	Audit      *PermissionAudit `json:"audit,omitempty"`
}

// PermissionAudit retains committed checked management intent, not current
// permission or human identity.
type PermissionAudit struct {
	Principal     string                `json:"principal"`
	Workspace     string                `json:"workspace"`
	Action        string                `json:"action"`
	Revision      string                `json:"revision"`
	ExpiresAt     time.Time             `json:"expires_at,omitzero"`
	Services      []routing.ServiceKind `json:"services,omitempty"`
	RequestLimits *[]RequestRateLimit   `json:"request_limits,omitempty"`
	CommittedAt   time.Time             `json:"committed_at"`
}

func validatePermissionReceipts(receipts []PermissionReceipt) error {
	seen := make(map[string]bool, len(receipts))
	for _, receipt := range receipts {
		if audit := receipt.Audit; audit != nil {
			if !validAuditCaller(receipt.Caller) || audit.Revision == "" || audit.CommittedAt.IsZero() || (audit.Action != "permission.grant" && audit.Action != "permission.revoke") || (audit.Action == "permission.revoke" && !audit.ExpiresAt.IsZero()) {
				return errors.New("invalid permission audit binding")
			}
			if _, err := accessidentity.Parse(audit.Principal); err != nil {
				return err
			}
			slug, err := routing.ParseWorkspaceSlug(audit.Workspace)
			if err != nil || slug.String() != audit.Workspace {
				return errors.New("invalid permission audit Workspace")
			}
			intent := fmt.Appendf(nil, "%t\x00%s\x00%s\x00%s", audit.Action == "permission.grant", audit.Principal, audit.Workspace, audit.Revision)
			if !audit.ExpiresAt.IsZero() {
				intent = fmt.Appendf(intent, "\x00%s", audit.ExpiresAt.UTC().Format(time.RFC3339Nano))
			}
			services, err := canonicalServices(audit.Services)
			if err != nil || (services != nil && !slices.Equal(services, audit.Services)) || (audit.Action == "permission.revoke" && services != nil) {
				return errors.New("invalid permission audit Service scope")
			}
			intent = append(intent, scopeFingerprint(services)...)
			if audit.RequestLimits != nil {
				rates, rateErr := canonicalRequestRates(*audit.RequestLimits, services)
				if rateErr != nil || !slices.Equal(rates, *audit.RequestLimits) {
					return errors.New("invalid permission audit request limits")
				}
				intent = append(intent, requestRateFingerprint(rates)...)
			}
			digest := sha256.Sum256(intent)
			if hex.EncodeToString(digest[:]) != receipt.IntentHash {
				return errors.New("permission audit does not match reviewed intent")
			}
		}
		if !validAuditCaller(receipt.Caller) {
			return errors.New("permission receipt has unsupported management caller")
		}
		for _, value := range []string{receipt.KeyHash, receipt.IntentHash} {
			decoded, err := hex.DecodeString(value)
			if err != nil || len(decoded) != sha256.Size || value != strings.ToLower(value) {
				return errors.New("invalid permission receipt digest")
			}
		}
		if seen[receipt.Caller+"\x00"+receipt.KeyHash] {
			return errors.New("duplicate permission receipt key")
		}
		seen[receipt.Caller+"\x00"+receipt.KeyHash] = true
	}
	return nil
}

// ApplyPermission commits the exact permission intent and replay receipt in one
// atomic authority publication. Replay precedes revision admission and never
// reapplies authority after a later revocation. The caller owns current local
// admission and Workspace existence serialized with Workspace destruction.
func (s *Store) ApplyPermission(id accessidentity.ID, workspace routing.WorkspaceSlug, expected, key string, grant bool, expiry ...time.Time) error {
	_, err := s.applyPermission(context.Background(), id, workspace, expected, key, grant, nil, nil, nil, expiry...)
	return err
}

// ApplyPermissionChecked repeats trusted Node admission under the authority
// publication lock before receipt lookup or fresh mutation. authorize must not
// reenter this Store: Workspace credential resolution is not Node admission.
// Expected binds the natural relationship revision, never the store revision.
func (s *Store) ApplyPermissionChecked(ctx context.Context, id accessidentity.ID, workspace routing.WorkspaceSlug, expected, key string, grant bool, authorize func() error, expiry ...time.Time) (PermissionMutation, error) {
	if authorize == nil {
		return PermissionMutation{}, errors.New("permission apply requires trusted admission")
	}
	if expected == "" {
		return PermissionMutation{}, errors.New("checked permission apply requires relationship revision")
	}
	return s.applyPermission(ctx, id, workspace, expected, key, grant, authorize, nil, nil, expiry...)
}

// ApplyPermissionServicesChecked is the reviewed, idempotent, closed-scope
// grant mutation. Omitting Services preserves an existing scoped grant,
// rather than silently widening it; a new Workspace-wide grant is explicit.
// The Web Operator always sends an explicit nonempty set.
func (s *Store) ApplyPermissionServicesChecked(ctx context.Context, id accessidentity.ID, workspace routing.WorkspaceSlug, expected, key string, grant bool, authorize func() error, services []routing.ServiceKind, expiry ...time.Time) (PermissionMutation, error) {
	if authorize == nil || expected == "" {
		return PermissionMutation{}, errors.New("checked Service permission requires admission and revision")
	}
	return s.applyPermission(ctx, id, workspace, expected, key, grant, authorize, services, nil, expiry...)
}

// ApplyPermissionServicesWithRatesChecked publishes a reviewed Service-scope
// and its per-Service RPM policy as *one* relationship edit/receipt. Nil limits
// preserve existing ceilings; an explicit empty slice removes them.
func (s *Store) ApplyPermissionServicesWithRatesChecked(ctx context.Context, id accessidentity.ID, workspace routing.WorkspaceSlug, expected, key string, authorize func() error, services []routing.ServiceKind, rates []RequestRateLimit, expiry ...time.Time) (PermissionMutation, error) {
	if authorize == nil || expected == "" {
		return PermissionMutation{}, errors.New("checked permission requires admission and revision")
	}
	return s.applyPermission(ctx, id, workspace, expected, key, true, authorize, services, rates, expiry...)
}

func (s *Store) applyPermission(ctx context.Context, id accessidentity.ID, workspace routing.WorkspaceSlug, expected, key string, grant bool, authorize func() error, services []routing.ServiceKind, rates []RequestRateLimit, expiry ...time.Time) (PermissionMutation, error) {
	if len(expiry) > 1 || (!grant && len(expiry) != 0) {
		return PermissionMutation{}, errors.New("expiry belongs to one permission grant")
	}
	services, err := canonicalServices(services)
	if err != nil {
		return PermissionMutation{}, err
	}
	if !grant && (services != nil || rates != nil) {
		return PermissionMutation{}, errors.New("revocation cannot carry Service scope or limits")
	}
	rates, err = canonicalRequestRates(rates, services)
	if err != nil {
		return PermissionMutation{}, err
	}
	var expiresAt time.Time
	if len(expiry) == 1 {
		expiresAt = expiry[0].UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Replay is a management response too: neither receipt possession nor a
	// formerly admitted request can bypass current transaction-time authority.
	if authorize != nil {
		if err := authorize(); err != nil {
			return PermissionMutation{}, err
		}
	}
	keySum := sha256.Sum256([]byte(key))
	operation := "revoke"
	if grant {
		operation = "grant"
	}
	intentSum := sha256.Sum256(fmt.Appendf(nil, "%t\x00%s\x00%s\x00%s", grant, id.String(), workspace.String(), expected))
	// A permission deadline is semantic intent. Normalize it to one instant
	// so retries with different deadlines cannot share an identity.
	if !expiresAt.IsZero() {
		intentSum = sha256.Sum256(fmt.Appendf(nil, "%t\x00%s\x00%s\x00%s\x00%s", grant, id.String(), workspace.String(), expected, expiresAt.Format(time.RFC3339Nano)))
	}
	if services != nil {
		intent := fmt.Appendf(nil, "%t\x00%s\x00%s\x00%s", grant, id.String(), workspace.String(), expected)
		if !expiresAt.IsZero() {
			intent = fmt.Appendf(intent, "\x00%s", expiresAt.Format(time.RFC3339Nano))
		}
		intent = append(intent, scopeFingerprint(services)...)
		intentSum = sha256.Sum256(intent)
	}
	if rates != nil {
		intent := fmt.Appendf(nil, "%t\x00%s\x00%s\x00%s", grant, id.String(), workspace.String(), expected)
		if !expiresAt.IsZero() {
			intent = fmt.Appendf(intent, "\x00%s", expiresAt.Format(time.RFC3339Nano))
		}
		intent = append(intent, scopeFingerprint(services)...)
		intent = append(intent, requestRateFingerprint(rates)...)
		intentSum = sha256.Sum256(intent)
	}
	receipt := PermissionReceipt{Caller: accessidentity.Attribution(ctx).String(), KeyHash: hex.EncodeToString(keySum[:]), IntentHash: hex.EncodeToString(intentSum[:])}
	if key != "" {
		for _, previous := range s.permissionReceipts {
			if previous.Caller == receipt.Caller && previous.KeyHash == receipt.KeyHash {
				if previous.IntentHash != receipt.IntentHash {
					return PermissionMutation{}, ErrPermissionIdempotencyConflict
				}
				return s.permissionMutationLocked(id, workspace, true)
			}
		}
	}
	revision := s.revisionLocked()
	if authorize != nil {
		// Checked Control consent covers exactly this relationship and impact;
		// accepting the store digest here would restore a competing contract.
		index, ok := s.byAccess[id]
		if !ok {
			return PermissionMutation{}, ErrNotFound
		}
		revision = permissionRevision(s.state[index], workspace)
	}
	if expected != "" && expected != revision {
		return PermissionMutation{}, ErrRevisionConflict
	}
	candidate, err := s.permissionCandidateWithRatesLocked(id, workspace, grant, expiresAt, services, rates)
	if err != nil {
		return PermissionMutation{}, err
	}
	receipts := append([]PermissionReceipt(nil), s.permissionReceipts...)
	if key != "" {
		if authorize != nil {
			audit := &PermissionAudit{Principal: id.String(), Workspace: workspace.String(), Action: "permission." + operation, Revision: expected, ExpiresAt: expiresAt, Services: services, CommittedAt: s.now().UTC()}
			if rates != nil {
				saved := append([]RequestRateLimit{}, rates...)
				audit.RequestLimits = &saved
			}
			receipt.Audit = audit
		}
		receipts = append(receipts, receipt)
	}
	// TODO: qualify bounded receipt retention against an owner-approved retry
	// envelope; do not silently evict historical uncertainty or replay guarantees.
	if err := s.commitWithPermissionReceipts(candidate, receipts); err != nil {
		return PermissionMutation{}, err
	}
	return s.permissionMutationLocked(id, workspace, false)
}

// PermissionMutation is current relationship truth captured under the apply
// lock. Replayed means no effect was repeated, not that historical authority
// remains active. Snapshot values are detached from retained authority.
type PermissionMutation struct {
	PermissionSnapshot
	Replayed bool
}

func (s *Store) permissionMutationLocked(id accessidentity.ID, workspace routing.WorkspaceSlug, replayed bool) (PermissionMutation, error) {
	snapshot, err := s.readPermissionLocked(id, workspace)
	if err != nil {
		return PermissionMutation{}, err
	}
	return PermissionMutation{PermissionSnapshot: snapshot, Replayed: replayed}, nil
}

// permissionCandidateLocked is shared by advisory preview and publication.
// It never mutates retained authority; callers hold the Access store lock.
func (s *Store) permissionCandidateLocked(id accessidentity.ID, workspace routing.WorkspaceSlug, grant bool, expiresAt time.Time, services ...[]routing.ServiceKind) ([]Access, error) {
	var scope []routing.ServiceKind
	if len(services) > 0 {
		scope = services[0]
	}

	return s.permissionCandidateWithRatesLocked(id, workspace, grant, expiresAt, scope, nil)
}

func (s *Store) permissionCandidateWithRatesLocked(id accessidentity.ID, workspace routing.WorkspaceSlug, grant bool, expiresAt time.Time, scope []routing.ServiceKind, rates []RequestRateLimit) ([]Access, error) {
	index, ok := s.byAccess[id]
	if !ok {
		return nil, ErrNotFound
	}
	if grant && s.state[index].RevokedAt != nil {
		return nil, ErrRevoked
	}
	candidate := cloneState(s.state)
	if grant {
		found := false
		for indexPermission := range candidate[index].Permissions {
			if candidate[index].Permissions[indexPermission].Workspace == workspace {
				candidate[index].Permissions[indexPermission].ExpiresAt = expiresAt
				// An older caller that omits Services must not turn an existing
				// Models-only permission into a Workspace-wide grant.
				if scope == nil {
					scope = candidate[index].Permissions[indexPermission].Services
				}
				if _, err := canonicalRequestRates(rates, scope); err != nil {
					return nil, err
				}
				candidate[index].Permissions[indexPermission].Services = append([]routing.ServiceKind(nil), scope...)
				chosen := rates
				if chosen == nil {
					chosen = candidate[index].Permissions[indexPermission].RequestLimits
				}
				if scope != nil {
					filtered := []RequestRateLimit{}
					for _, rate := range chosen {
						if slices.Contains(scope, rate.Service) {
							filtered = append(filtered, rate)
						}
					}
					chosen = filtered
				}
				candidate[index].Permissions[indexPermission].RequestLimits = append([]RequestRateLimit(nil), chosen...)
				found = true
				break
			}
		}
		if !found {
			candidate[index].Permissions = append(candidate[index].Permissions, Permission{Workspace: workspace, Services: append([]routing.ServiceKind(nil), scope...), RequestLimits: append([]RequestRateLimit(nil), rates...), ExpiresAt: expiresAt})
		}
	} else {
		permissions := candidate[index].Permissions[:0]
		for _, current := range candidate[index].Permissions {
			if current.Workspace != workspace {
				permissions = append(permissions, current)
			}
		}
		candidate[index].Permissions = permissions
	}
	return candidate, nil
}

// PermissionImpact is an effect-free snapshot of one proposed relationship
// edit. Revision, prior and proposed state are read under the same authority lock.
// It is advisory and cannot confer approval or reserve an idempotency key.
type PermissionImpact struct {
	Revision        string
	Before          *Permission
	After           *Permission
	CredentialCount int
}

// PermissionSnapshot is current relationship truth, including an absent grant
// on an existing principal. Its revision and effective decision share one lock.
type PermissionSnapshot struct {
	Revision   string
	Permission *Permission
	Allowed    bool
}

// ReadPermission observes authority without candidate construction, receipt
// lookup or publication. Expired relationships remain inspectable metadata.
func (s *Store) ReadPermission(id accessidentity.ID, workspace routing.WorkspaceSlug) (PermissionSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readPermissionLocked(id, workspace)
}

func (s *Store) readPermissionLocked(id accessidentity.ID, workspace routing.WorkspaceSlug) (PermissionSnapshot, error) {
	index, ok := s.byAccess[id]
	if !ok {
		return PermissionSnapshot{}, ErrNotFound
	}
	access := s.state[index]
	snapshot := PermissionSnapshot{Revision: permissionRevision(access, workspace), Allowed: access.RevokedAt == nil && permissionAllows(access, workspace, s.now())}
	for _, permission := range access.Permissions {
		if permission.Workspace == workspace {
			value := clonePermission(permission)
			snapshot.Permission = &value
			break
		}
	}
	return snapshot, nil
}

// PreviewPermission uses the same candidate validation as apply, without
// publishing authority, receipts, or credentials. Expected may be omitted for
// discovery; when supplied it checks the same revision as a fresh apply.
func (s *Store) PreviewPermission(id accessidentity.ID, workspace routing.WorkspaceSlug, expected string, grant bool, expiresAt time.Time) (PermissionImpact, error) {
	return s.PreviewPermissionServices(id, workspace, expected, grant, expiresAt, nil)
}

func (s *Store) PreviewPermissionServices(id accessidentity.ID, workspace routing.WorkspaceSlug, expected string, grant bool, expiresAt time.Time, services []routing.ServiceKind) (PermissionImpact, error) {
	return s.PreviewPermissionServicesWithRates(id, workspace, expected, grant, expiresAt, services, nil)
}

func (s *Store) PreviewPermissionServicesWithRates(id accessidentity.ID, workspace routing.WorkspaceSlug, expected string, grant bool, expiresAt time.Time, services []routing.ServiceKind, rates []RequestRateLimit) (PermissionImpact, error) {
	services, err := canonicalServices(services)
	if err != nil {
		return PermissionImpact{}, err
	}
	if !grant && (services != nil || rates != nil) {
		return PermissionImpact{}, errors.New("revocation cannot carry Service scope or limits")
	}
	rates, err = canonicalRequestRates(rates, services)
	if err != nil {
		return PermissionImpact{}, err
	}
	if !grant && !expiresAt.IsZero() {
		return PermissionImpact{}, errors.New("expiry belongs to one permission grant")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	index, ok := s.byAccess[id]
	if !ok {
		return PermissionImpact{}, ErrNotFound
	}
	revision := permissionRevision(s.state[index], workspace)
	if expected != "" && expected != revision {
		return PermissionImpact{}, ErrRevisionConflict
	}
	candidate, err := s.permissionCandidateWithRatesLocked(id, workspace, grant, expiresAt.UTC(), services, rates)
	if err != nil {
		return PermissionImpact{}, err
	}
	impact := PermissionImpact{Revision: revision}
	for _, permission := range s.state[index].Permissions {
		if permission.Workspace == workspace {
			value := clonePermission(permission)
			impact.Before = &value
			break
		}
	}
	for _, permission := range candidate[index].Permissions {
		if permission.Workspace == workspace {
			value := clonePermission(permission)
			impact.After = &value
			break
		}
	}
	for _, credential := range s.state[index].Credentials {
		if credential.Workspace == workspace {
			impact.CredentialCount++
		}
	}
	return impact, nil
}
