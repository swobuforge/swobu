package connections

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

var ErrIdempotencyConflict = errors.New("Connection retry key reused with different intent")

// These bounds reuse the Node's existing local management retry envelope in
// configstore. They protect registry memory and document size. At capacity,
// the oldest expiry is evicted atomically with the next successful creation.
const creationReceiptCapacity = 4096
const creationReceiptTTL = 24 * time.Hour

type creationReceipt struct {
	Intent    string    `json:"intent"`
	ExpiresAt time.Time `json:"expires_at"`
	Result    record    `json:"result"`
}

func validateCreationReceipts(receipts map[string]creationReceipt, supports routing.ProviderSupport) error {
	if len(receipts) > creationReceiptCapacity {
		return errors.New("too many Connection creation receipts")
	}
	for key, receipt := range receipts {
		for _, value := range []string{key, receipt.Intent} {
			raw, err := hex.DecodeString(value)
			if err != nil || len(raw) != sha256.Size || hex.EncodeToString(raw) != value {
				return errors.New("invalid Connection receipt digest")
			}
		}
		result, err := decodeRecord(receipt.Result, supports)
		if err != nil || result.Revision().value != 1 || !result.CreatedAt().Equal(result.UpdatedAt()) || !result.CreatedAt().Add(creationReceiptTTL).Equal(receipt.ExpiresAt) {
			return errors.New("invalid Connection creation receipt")
		}
	}
	return nil
}

// CreateChecked generates identity once and atomically commits both the new
// configuration and original-result receipt. Trusted caller scope isolates retry
// keys; admission repeats on replay under the publication lock. The admission
// callback must not reenter the registry. Public APIs must project the returned
// value through their redacted read model, never serialize receipt records.
func (s *Store) CreateChecked(ctx context.Context, label string, spec Spec, scope, key string, authorize func(context.Context) error) (Connection, bool, error) {
	if scope == "" || key == "" || authorize == nil {
		return Connection{}, false, errors.New("Connection creation requires trusted scope, retry key and admission")
	}
	// Canonicalize validated spec semantics without random identity or wall time.
	draftID, err := connectionidentity.Parse("draft")
	if err != nil {
		return Connection{}, false, err
	}
	probe, err := New(draftID, label, spec, time.Unix(0, 0))
	if err != nil {
		return Connection{}, false, err
	}
	canonical, err := encodeRecord(probe)
	if err != nil {
		return Connection{}, false, err
	}
	raw, _ := json.Marshal(struct {
		Label string
		Kind  Kind
		Spec  json.RawMessage
	}{label, spec.Kind(), canonical.Spec})
	intentSum := sha256.Sum256(raw)
	raw, _ = json.Marshal(struct{ Scope, Method, Resource, Key string }{scope, "POST", "/api/v1/connections", key})
	keySum := sha256.Sum256(raw)
	intent, keyHash := hex.EncodeToString(intentSum[:]), hex.EncodeToString(keySum[:])
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
	if receipt, exists := s.creations[keyHash]; exists && now.Before(receipt.ExpiresAt) {
		if receipt.Intent != intent {
			return Connection{}, false, ErrIdempotencyConflict
		}
		// The receipt was validated at creation or load, with provider admission.
		original, err := decodeRecord(receipt.Result, func(string) bool { return true })
		return original, true, err
	}
	id, err := connectionidentity.New()
	if err != nil {
		return Connection{}, false, err
	}
	if _, exists := s.values[id]; exists {
		return Connection{}, false, ErrAlreadyExists
	}
	candidate, err := New(id, label, spec, now)
	if err != nil {
		return Connection{}, false, err
	}
	result, err := encodeRecord(candidate)
	if err != nil {
		return Connection{}, false, err
	}
	receipts := make(map[string]creationReceipt, len(s.creations)+1)
	for k, v := range s.creations {
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
	receipts[keyHash] = creationReceipt{intent, now.Add(creationReceiptTTL), result}
	values := make(map[connectionidentity.ID]Connection, len(s.values)+1)
	for k, v := range s.values {
		values[k] = v
	}
	values[id] = candidate
	if err := ctx.Err(); err != nil {
		return Connection{}, false, err
	}
	if err := s.publishDocument(values, receipts, appendAudit(s.audits, Audit{Action: "connection.create", Caller: scope, ConnectionID: id.String(), AfterRevision: candidate.Revision().String(), CommittedAt: now})); err != nil {
		return Connection{}, false, err
	}
	return candidate, false, nil
}
