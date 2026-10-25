package connections

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

// CredentialStageLease is the local management abandonment/retry envelope.
const CredentialStageLease = creationReceiptTTL

// StagedCredential is private cleanup metadata, never secret material or a
// public response. Each backend candidate is recorded before storage runs;
// edit/delete/replacement also track preexisting owned bindings after removal.
// ExpiresAt uses CredentialStageLease; live references always override cleanup
// eligibility after lease expiry.
type StagedCredential struct {
	ConnectionID string    `json:"connection_id"`
	Reference    string    `json:"reference"`
	ExpiresAt    time.Time `json:"expires_at"`
	RetryKey     string    `json:"retry_key,omitempty"`
	Intent       string    `json:"intent,omitempty"`
}

// trackRemovedOwnedBindings closes cleanup inventory at the atomic document
// boundary for edits, deletion and credential replacement alike. It adds only
// this Connection's immutable slots removed from its live spec; existing retry
// leases remain untouched. Shared users are excluded later under publication
// locks by retirement, never inferred from this local transition.
func trackRemovedOwnedBindings(before, after map[connectionidentity.ID]Connection, staged map[string]StagedCredential, now time.Time) map[string]StagedCredential {
	var tracked map[string]StagedCredential
	for id, previous := range before {
		current := after[id]
		for _, ref := range previous.CredentialRefs() {
			if !OwnedCredentialSlot(id, ref) || current.spec != nil && usesRef(current, ref) {
				continue
			}
			if _, exists := staged[ref.String()]; exists {
				continue
			}
			if tracked == nil {
				tracked = make(map[string]StagedCredential, len(staged)+1)
				for key, entry := range staged {
					tracked[key] = entry
				}
			}
			tracked[ref.String()] = StagedCredential{ConnectionID: id.String(), Reference: ref.String(), ExpiresAt: now.Add(CredentialStageLease)}
		}
	}
	if tracked != nil {
		return tracked
	}
	return staged
}

func validateStaging(staged map[string]StagedCredential) error {
	retries := map[string]StagedCredential{}
	for key, value := range staged {
		id, err := connectionidentity.Parse(value.ConnectionID)
		if err != nil || key != value.Reference || !OwnedCredentialSlot(id, credentialref.Parse(value.Reference)) || value.ExpiresAt.IsZero() {
			return errors.New("invalid staged Connection credential")
		}
		if (value.RetryKey == "") != (value.Intent == "") {
			return errors.New("invalid staged Connection retry")
		}
		if value.RetryKey != "" {
			if previous, exists := retries[value.RetryKey]; exists {
				_, previousSlot, _ := strings.Cut(previous.Reference, ":")
				_, slot, _ := strings.Cut(value.Reference, ":")
				if previous.ConnectionID != value.ConnectionID || previous.Intent != value.Intent || !previous.ExpiresAt.Equal(value.ExpiresAt) || previousSlot != slot {
					return errors.New("inconsistent staged Connection retry")
				}
			}
			retries[value.RetryKey] = value
			for _, digest := range []string{value.RetryKey, value.Intent} {
				raw, err := hex.DecodeString(digest)
				if err != nil || len(raw) != sha256.Size || hex.EncodeToString(raw) != digest {
					return errors.New("invalid staged Connection retry digest")
				}
			}
		}
	}
	return nil
}

// StageCredential records all generated backend candidates before storage is
// allowed to run. The storage callback receives no registry authority and cannot
// reenter this store. An error leaves durable metadata for cleanup; selected
// must be one of the recorded slots. All candidates must belong to id.
func (s *Store) StageCredential(ctx context.Context, id connectionidentity.ID, candidates []credentialref.Ref, authorize func(context.Context) error, write func(context.Context) (credentialref.Ref, error)) (credentialref.Ref, error) {
	if authorize == nil || write == nil || len(candidates) == 0 {
		return credentialref.Ref{}, errors.New("credential staging requires candidates, admission and storage")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return credentialref.Ref{}, err
	}
	if err := authorize(ctx); err != nil {
		return credentialref.Ref{}, err
	}
	if err := ctx.Err(); err != nil {
		return credentialref.Ref{}, err
	}
	if _, exists := s.values[id]; !exists {
		return credentialref.Ref{}, ErrNotFound
	}
	staged := make(map[string]StagedCredential, len(s.staged)+len(candidates))
	for key, value := range s.staged {
		staged[key] = value
	}
	expires := time.Now().UTC().Add(CredentialStageLease)
	for _, ref := range candidates {
		if !OwnedCredentialSlot(id, ref) {
			return credentialref.Ref{}, errors.New("staging requires generated owned slots")
		}
		if _, exists := staged[ref.String()]; exists {
			return credentialref.Ref{}, errors.New("credential slot already staged")
		}
		if _, exists := s.retired[ref.String()]; exists {
			return credentialref.Ref{}, ErrCredentialRetired
		}
		for _, value := range s.values {
			if usesRef(value, ref) {
				return credentialref.Ref{}, ErrCredentialInUse
			}
		}
		staged[ref.String()] = StagedCredential{ConnectionID: id.String(), Reference: ref.String(), ExpiresAt: expires}
	}
	if err := s.writeStaging(staged); err != nil {
		return credentialref.Ref{}, err
	}
	if err := ctx.Err(); err != nil {
		return credentialref.Ref{}, err
	}
	selected, err := write(ctx)
	if err != nil {
		return credentialref.Ref{}, errors.New("cannot store staged Connection credential")
	}
	for _, ref := range candidates {
		if ref.String() == selected.String() {
			return selected, nil
		}
	}
	return credentialref.Ref{}, errors.New("storage returned an unregistered credential slot")
}

// StagedCredentials returns detached cleanup metadata, sorted by locator. It
// is internal state, not operator-facing configuration or readiness evidence.
func (s *Store) StagedCredentials() []StagedCredential {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]StagedCredential, 0, len(s.staged))
	for _, value := range s.staged {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Reference < result[j].Reference })
	return result
}

// ReconcileStagedCredentials reclaims only expired unreferenced candidates.
// Retirement first persists the no-rebind marker and excludes live config.
// Storage failures retain the staged entry for retry and do not starve other
// candidates. Cancellation stops the pass. Shared/live entries stay
// tracked until their final reference disappears. Retry snapshots are history.
func (l *Lifecycle) ReconcileStagedCredentials(ctx context.Context, now time.Time, retire func(context.Context, connectionidentity.ID, credentialref.Ref) error) error {
	if retire == nil {
		return errors.New("credential retirement adapter is required")
	}
	var failures []error
	for _, entry := range l.registry.StagedCredentials() {
		if now.Before(entry.ExpiresAt) {
			continue
		}
		id, _ := connectionidentity.Parse(entry.ConnectionID)
		err := l.RetireCredentialIfUnused(ctx, id, credentialref.Parse(entry.Reference), retire)
		if errors.Is(err, ErrCredentialInUse) {
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			failures = append(failures, err)
			continue
		}
		l.registry.mu.Lock()
		staged := make(map[string]StagedCredential, len(l.registry.staged))
		for key, value := range l.registry.staged {
			if key != entry.Reference {
				staged[key] = value
			}
		}
		err = l.registry.writeStaging(staged)
		l.registry.mu.Unlock()
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// StageCredentialRetry binds one generated candidate pair to caller-scoped
// intent before storage. Retries recover the same slot across restart. Intent
// contains only command metadata; material equality is enforced by the storage
// callback inside its credential seam. The callback must not reenter this store.
// Fresh is false after any durable reservation, even when the first storage
// outcome is unknown, so recovery cannot assume that material is absent.
func (s *Store) StageCredentialRetry(ctx context.Context, id connectionidentity.ID, expected Revision, environment, scope, key string, authorize func(context.Context) error, allocate func() ([]credentialref.Ref, error), write func(context.Context, []credentialref.Ref, bool) (credentialref.Ref, error)) (credentialref.Ref, error) {
	if id.String() == "" || expected.value == 0 || scope == "" || key == "" || authorize == nil || allocate == nil || write == nil {
		return credentialref.Ref{}, errors.New("credential staging retry requires reviewed intent and admission")
	}
	digest := func(value any) string {
		raw, _ := json.Marshal(value)
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:])
	}
	retryKey := digest(struct{ Scope, ID, Key string }{scope, id.String(), key})
	intent := digest(struct{ ID, Revision, Environment string }{id.String(), expected.String(), environment})
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return credentialref.Ref{}, err
	}
	if err := authorize(ctx); err != nil {
		return credentialref.Ref{}, err
	}
	if err := ctx.Err(); err != nil {
		return credentialref.Ref{}, err
	}
	now := time.Now().UTC()
	candidates := []credentialref.Ref{}
	for _, stage := range s.staged {
		if stage.RetryKey != retryKey || !now.Before(stage.ExpiresAt) {
			continue
		}
		if stage.Intent != intent || stage.ConnectionID != id.String() {
			return credentialref.Ref{}, ErrIdempotencyConflict
		}
		if _, retired := s.retired[stage.Reference]; retired {
			return credentialref.Ref{}, ErrCredentialRetired
		}
		candidates = append(candidates, credentialref.Parse(stage.Reference))
	}
	fresh := len(candidates) == 0
	if fresh {
		current, exists := s.values[id]
		if !exists {
			return credentialref.Ref{}, ErrNotFound
		}
		if current.Revision() != expected {
			return credentialref.Ref{}, ErrRevisionConflict
		}
		if _, err := current.CredentialBinding(environment); err != nil {
			return credentialref.Ref{}, err
		}
		var err error
		candidates, err = allocate()
		if err != nil {
			return credentialref.Ref{}, err
		}
		if len(candidates) == 0 {
			return credentialref.Ref{}, errors.New("credential staging candidates required")
		}
		staged := make(map[string]StagedCredential, len(s.staged)+len(candidates))
		for ref, stage := range s.staged {
			staged[ref] = stage
		}
		for _, ref := range candidates {
			if !OwnedCredentialSlot(id, ref) {
				return credentialref.Ref{}, ErrCredentialBinding
			}
			if _, exists := staged[ref.String()]; exists {
				return credentialref.Ref{}, ErrCredentialBinding
			}
			if _, retired := s.retired[ref.String()]; retired {
				return credentialref.Ref{}, ErrCredentialRetired
			}
			for _, value := range s.values {
				if usesRef(value, ref) {
					return credentialref.Ref{}, ErrCredentialInUse
				}
			}
			staged[ref.String()] = StagedCredential{ConnectionID: id.String(), Reference: ref.String(), ExpiresAt: now.Add(CredentialStageLease), RetryKey: retryKey, Intent: intent}
		}
		if err := s.writeStaging(staged); err != nil {
			return credentialref.Ref{}, err
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].String() < candidates[j].String() })
	if err := ctx.Err(); err != nil {
		return credentialref.Ref{}, err
	}
	selected, err := write(ctx, append([]credentialref.Ref(nil), candidates...), fresh)
	if err != nil {
		return credentialref.Ref{}, err
	}
	for _, ref := range candidates {
		if selected == ref {
			return selected, nil
		}
	}
	return credentialref.Ref{}, ErrCredentialBinding
}
