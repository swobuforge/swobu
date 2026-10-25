package accessstate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
)

// PrincipalRevision binds stable identity and its publication-owned generation.
// All edits of this Principal invalidate consent; sibling edits do not. It never
// exposes secret material or treats the whole-store ETag as resource authority.
func PrincipalRevision(identity Access) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "principal\x00%s\x00%d", identity.ID.String(), identity.AuthorityGeneration))
	return hex.EncodeToString(sum[:])
}

// UpdatePrincipalLabelChecked compares and publishes under the Access lock.
// Admission must not reenter the store. Only label changes; creation receipt,
// credential digests, permissions and revoked status are preserved exactly.
func (s *Store) UpdatePrincipalLabelChecked(ctx context.Context, id accessidentity.ID, label, expected string, authorize func() error) (Access, error) {
	label, err := validateName(label)
	if err != nil {
		return Access{}, err
	}
	if expected == "" || authorize == nil {
		return Access{}, errors.New("Principal update requires revision and trusted admission")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := authorize(); err != nil {
		return Access{}, err
	}
	index, exists := s.byAccess[id]
	if !exists {
		return Access{}, ErrNotFound
	}
	if PrincipalRevision(s.state[index]) != expected {
		return Access{}, ErrRevisionConflict
	}
	if s.state[index].Name == label {
		return cloneAccess(s.state[index]), nil
	}
	candidate := cloneState(s.state)
	candidate[index].Name = label
	candidate[index].LabelAudits = append(candidate[index].LabelAudits, PrincipalLabelAudit{Principal: id.String(), Caller: accessidentity.Attribution(ctx).String(), Before: s.state[index].Name, After: label, Revision: expected, Generation: s.state[index].AuthorityGeneration, CommittedAt: s.now().UTC()})
	if err := s.commit(candidate); err != nil {
		return Access{}, err
	}
	return cloneAccess(s.state[s.byAccess[id]]), nil
}

// PreviewPrincipalRevocation returns a detached current impact snapshot under
// the same revision/admission check as revocation. It publishes nothing and
// confers no approval. Admission callbacks must not reenter the store.
func (s *Store) PreviewPrincipalRevocation(id accessidentity.ID, expected string, authorize func() error) (Access, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.checkedPrincipalIndex(id, expected, authorize)
	if err != nil {
		return Access{}, err
	}
	return cloneAccess(s.state[index]), nil
}

// RevokePrincipalChecked removes all use authority while retaining attribution
// in one Access publication. This internal transaction does not supply network
// idempotency receipts; a Control consumer must retain that separate contract.
// Revision and trusted admission are checked before even a revoked no-op.
func (s *Store) RevokePrincipalChecked(id accessidentity.ID, expected string, authorize func() error) (Access, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index, err := s.checkedPrincipalIndex(id, expected, authorize)
	if err != nil {
		return Access{}, err
	}
	if s.state[index].RevokedAt != nil {
		return cloneAccess(s.state[index]), nil
	}
	candidate := cloneState(s.state)
	now := s.now().UTC()
	candidate[index].RevokedAt = &now
	candidate[index].Credentials = nil
	candidate[index].Permissions = nil
	if err := s.commit(candidate); err != nil {
		return Access{}, err
	}
	return cloneAccess(s.state[s.byAccess[id]]), nil
}

// checkedPrincipalIndex runs only with mu held, keeping impact and mutation
// admission on one boundary. Whole-store revisions cannot substitute here.
func (s *Store) checkedPrincipalIndex(id accessidentity.ID, expected string, authorize func() error) (int, error) {
	if expected == "" || authorize == nil {
		return 0, errors.New("Principal revocation requires revision and trusted admission")
	}
	if err := authorize(); err != nil {
		return 0, err
	}
	index, exists := s.byAccess[id]
	if !exists {
		return 0, ErrNotFound
	}
	if PrincipalRevision(s.state[index]) != expected {
		return 0, ErrRevisionConflict
	}
	return index, nil
}

// ApplyPrincipalRevocation publishes caller-scoped retry metadata with authority.
// Replay checks admission before receipt lookup and returns current metadata;
// it never repeats an effect or substitutes fresh consent for original intent.
// Receipts follow retained identity attribution, without plaintext keys or expiry.
func (s *Store) ApplyPrincipalRevocation(id accessidentity.ID, expected, key string, authorize func() error) (Access, bool, error) {
	if expected == "" || authorize == nil || len(key) == 0 || len(key) > 256 || strings.ContainsAny(key, " \t\r\n") {
		return Access{}, false, errors.New("Principal revocation requires revision, trusted admission and one retry key")
	}
	keySum := sha256.Sum256([]byte(accessidentity.Local.String() + "\x00DELETE\x00principals/" + id.String() + "\x00" + key))
	intentSum := sha256.Sum256([]byte("DELETE\x00principals/" + id.String() + "\x00" + expected))
	keyHash, intentHash := hex.EncodeToString(keySum[:]), hex.EncodeToString(intentSum[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := authorize(); err != nil {
		return Access{}, false, err
	}
	index, exists := s.byAccess[id]
	if !exists {
		return Access{}, false, ErrNotFound
	}
	if original, exists := s.state[index].RevocationReceipts[keyHash]; exists {
		if original != intentHash {
			return Access{}, false, ErrPrincipalIdempotencyConflict
		}
		return cloneAccess(s.state[index]), true, nil
	}
	if PrincipalRevision(s.state[index]) != expected {
		return Access{}, false, ErrRevisionConflict
	}
	candidate := cloneState(s.state)
	if candidate[index].RevokedAt == nil {
		now := s.now().UTC()
		candidate[index].RevokedAt = &now
		candidate[index].Credentials = nil
		candidate[index].Permissions = nil
	}
	if candidate[index].RevocationReceipts == nil {
		candidate[index].RevocationReceipts = make(map[string]string)
	}
	// TODO: qualify a retention envelope for revocation receipts before adding
	// eviction; dropping an uncertain original intent changes recovery guarantees.
	candidate[index].RevocationReceipts[keyHash] = intentHash
	if candidate[index].RevocationAudits == nil {
		candidate[index].RevocationAudits = make(map[string]PrincipalRevocationAudit)
	}
	candidate[index].RevocationAudits[keyHash] = principalRevocationAudit(s.state[index], expected, s.now())
	if err := s.commit(candidate); err != nil {
		return Access{}, false, err
	}
	return cloneAccess(s.state[s.byAccess[id]]), false, nil
}
