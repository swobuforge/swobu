package accessstate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"strings"
)

var ErrPrincipalIdempotencyConflict = errors.New("idempotency key conflicts with a different Principal creation")

// CreatePrincipalChecked publishes identity and local creation receipt together.
// No permission or credential is minted. Admission repeats under the publication
// lock before replay; callbacks must not reenter this store. Matching replay
// returns current identity metadata, never restores revoked authority. Receipts
// follow the identity's retained attribution lifetime rather than being evicted.
func (s *Store) CreatePrincipalChecked(ctx context.Context, label, key string, authorize func() error) (Access, bool, error) {
	label, err := validateName(label)
	if err != nil {
		return Access{}, false, err
	}
	if authorize == nil || len(key) == 0 || len(key) > 256 || strings.ContainsAny(key, " \t\r\n") {
		return Access{}, false, errors.New("Principal creation requires trusted admission and one retry key")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := authorize(); err != nil {
		return Access{}, false, err
	}
	keySum := sha256.Sum256([]byte(accessidentity.Attribution(ctx).String() + "\x00POST\x00principals\x00" + key))
	intentSum := sha256.Sum256([]byte("POST\x00principals\x00" + label))
	keyHash, intentHash := hex.EncodeToString(keySum[:]), hex.EncodeToString(intentSum[:])
	for _, identity := range s.state {
		if identity.CreationKeyHash != keyHash {
			continue
		}
		if identity.CreationIntentHash != intentHash {
			return Access{}, false, ErrPrincipalIdempotencyConflict
		}
		return cloneAccess(identity), true, nil
	}
	id, err := s.newAccessID()
	if err != nil {
		return Access{}, false, err
	}
	identity := Access{ID: id, Name: label, CreatedAt: s.now().UTC(), CreationKeyHash: keyHash, CreationIntentHash: intentHash}
	identity.CreationAudit = &PrincipalCreationAudit{Principal: id.String(), Caller: accessidentity.Attribution(ctx).String(), Label: label, CommittedAt: identity.CreatedAt}
	if err := s.commit(append(cloneState(s.state), identity)); err != nil {
		return Access{}, false, err
	}
	return cloneAccess(s.state[s.byAccess[id]]), false, nil
}
