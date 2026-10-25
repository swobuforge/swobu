package connections

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/routing"
)

type replacementReceipt struct {
	Intent    string    `json:"intent"`
	ExpiresAt time.Time `json:"expires_at"`
	Result    record    `json:"result"`
}

func validateReplacementReceipts(receipts map[string]replacementReceipt, supports routing.ProviderSupport) error {
	if len(receipts) > creationReceiptCapacity {
		return errors.New("too many Connection replacement receipts")
	}
	for key, receipt := range receipts {
		for _, value := range []string{key, receipt.Intent} {
			raw, err := hex.DecodeString(value)
			if err != nil || len(raw) != sha256.Size || hex.EncodeToString(raw) != value {
				return errors.New("invalid Connection replacement receipt digest")
			}
		}
		result, err := decodeRecord(receipt.Result, supports)
		if err != nil || result.Revision().value < 2 || !result.UpdatedAt().Add(creationReceiptTTL).Equal(receipt.ExpiresAt) {
			return errors.New("invalid Connection replacement receipt")
		}
	}
	return nil
}

// ReplaceCredentialChecked commits an already stored new locator, original-result
// retry receipt and management fact together. No secret material is accepted or
// resolved here. Scope is trusted caller attribution; admission repeats on replay
// under the publication lock and must not reenter this store. The application
// must stage immutable new material before this call and retire old material only
// after successful commit and a complete live-reference inspection.
func (s *Store) ReplaceCredentialChecked(ctx context.Context, id connectionidentity.ID, expected Revision, environment string, old, replacement credentialref.Ref, scope, key string, authorize func(context.Context) error) (Connection, bool, error) {
	return s.replaceCredentialChecked(ctx, id, expected, environment, old, replacement, scope, key, authorize, false)
}

// ReplaceStagedCredentialChecked selects the reviewed binding internally so raw
// secret APIs never need to expose or accept private old locators. Receipt
// intent excludes that derived field, making replay independent of current state.
func (s *Store) ReplaceStagedCredentialChecked(ctx context.Context, id connectionidentity.ID, expected Revision, environment string, replacement credentialref.Ref, scope, key string, authorize func(context.Context) error) (Connection, bool, error) {
	if !OwnedCredentialSlot(id, replacement) {
		return Connection{}, false, ErrCredentialBinding
	}
	return s.replaceCredentialChecked(ctx, id, expected, environment, credentialref.Ref{}, replacement, scope, key, authorize, true)
}

func (s *Store) replaceCredentialChecked(ctx context.Context, id connectionidentity.ID, expected Revision, environment string, old, replacement credentialref.Ref, scope, key string, authorize func(context.Context) error, deriveOld bool) (Connection, bool, error) {
	if scope == "" || key == "" || authorize == nil {
		return Connection{}, false, errors.New("Connection replacement requires trusted scope, retry key and admission")
	}
	if id.String() == "" || expected.value == 0 || (!deriveOld && (validateCredential(old) != nil || old.Kind() == credentialref.KindEmpty)) || validateCredential(replacement) != nil || replacement.Kind() == credentialref.KindEmpty || old == replacement {
		return Connection{}, false, ErrCredentialBinding
	}
	raw, _ := json.Marshal(struct{ ID, Revision, Environment, Old, Replacement string }{id.String(), expected.String(), environment, old.String(), replacement.String()})
	sum := sha256.Sum256(raw)
	intent := hex.EncodeToString(sum[:])
	raw, _ = json.Marshal(struct{ Scope, Method, Resource, Key string }{scope, "POST", "/api/v1/connections/" + id.String() + "/credential-replacements", key})
	sum = sha256.Sum256(raw)
	keyHash := hex.EncodeToString(sum[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Connection{}, false, err
	}
	if err := authorize(ctx); err != nil {
		return Connection{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return Connection{}, false, err
	}
	now := time.Now().UTC()
	if receipt, exists := s.replacements[keyHash]; exists && now.Before(receipt.ExpiresAt) {
		if receipt.Intent != intent {
			return Connection{}, false, ErrIdempotencyConflict
		}
		original, err := decodeRecord(receipt.Result, func(string) bool { return true })
		return original, true, err
	}
	current, exists := s.values[id]
	if !exists {
		return Connection{}, false, ErrNotFound
	}
	if deriveOld {
		var err error
		old, err = current.CredentialBinding(environment)
		if err != nil {
			return Connection{}, false, err
		}
	}
	candidate, err := current.ReplaceCredential(expected, environment, old, replacement, now)
	if err != nil {
		return Connection{}, false, err
	}
	result, err := encodeRecord(candidate)
	if err != nil {
		return Connection{}, false, err
	}
	receipts := make(map[string]replacementReceipt, len(s.replacements)+1)
	for k, v := range s.replacements {
		if now.Before(v.ExpiresAt) {
			receipts[k] = v
		}
	}
	if len(receipts) >= creationReceiptCapacity {
		oldest := ""
		for k, v := range receipts {
			if oldest == "" || v.ExpiresAt.Before(receipts[oldest].ExpiresAt) || (v.ExpiresAt.Equal(receipts[oldest].ExpiresAt) && k < oldest) {
				oldest = k
			}
		}
		delete(receipts, oldest)
	}
	receipts[keyHash] = replacementReceipt{intent, now.Add(creationReceiptTTL), result}
	values := make(map[connectionidentity.ID]Connection, len(s.values))
	for k, v := range s.values {
		values[k] = v
	}
	values[id] = candidate
	audits := appendAudit(s.audits, Audit{Action: "connection.credential_replace", Caller: scope, ConnectionID: id.String(), BeforeRevision: current.Revision().String(), AfterRevision: candidate.Revision().String(), CommittedAt: now})
	if err := ctx.Err(); err != nil {
		return Connection{}, false, err
	}
	// Keep every backend candidate available for the entire committed replay
	// envelope. Staging may precede commit by hours; its original abandonment
	// lease cannot determine cleanup eligibility for a newer retained receipt.
	staged := make(map[string]StagedCredential, len(s.staged)+1)
	for ref, entry := range s.staged {
		staged[ref] = entry
	}
	if selected, exists := s.staged[replacement.String()]; exists && selected.RetryKey != "" {
		for ref, entry := range s.staged {
			if entry.RetryKey == selected.RetryKey {
				entry.ExpiresAt = now.Add(creationReceiptTTL)
			}
			staged[ref] = entry
		}
	}
	if err := s.writeDocument(values, s.creations, receipts, audits, s.retired, staged); err != nil {
		return Connection{}, false, err
	}
	return candidate, false, nil
}
