package configstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/swobuforge/swobu/internal/routing"
	"gopkg.in/yaml.v3"
)

var ErrStaleRevision = errors.New("Workspace management ETag changed")
var ErrIdempotencyConflict = errors.New("Workspace idempotency key reused with different intent")

// The local routing owner retains a bounded replay window, not perpetual
// tombstones. Expiry/eviction ends replay protection and requires fresh state.
const deletionReceiptCapacity = 4096
const deletionReceiptTTL = 24 * time.Hour

type deletionReceipt struct {
	Intent    string                  `yaml:"intent"`
	ExpiresAt time.Time               `yaml:"expires_at"`
	Audit     *WorkspaceDeletionAudit `yaml:"audit,omitempty"`
}

// WorkspaceDeletionAudit records checked Routing removal, not independently
// committed Access revocations or current state of a reused slug.
type WorkspaceDeletionAudit struct {
	Action      string    `yaml:"action" json:"action"`
	Caller      string    `yaml:"caller" json:"caller"`
	Workspace   string    `yaml:"workspace" json:"workspace"`
	ETag        string    `yaml:"etag" json:"etag"`
	CommittedAt time.Time `yaml:"committed_at" json:"committed_at"`
}

func decodeDeletionReceipts(raw []byte) (map[string]deletionReceipt, error) {
	var dto documentDTO
	if err := yaml.Unmarshal(raw, &dto); err != nil {
		return nil, err
	}
	if len(dto.Deletions) > deletionReceiptCapacity {
		return nil, errors.New("too many Workspace deletion receipts")
	}
	for key, receipt := range dto.Deletions {
		for _, value := range []string{key, receipt.Intent} {
			decoded, err := hex.DecodeString(value)
			if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != value {
				return nil, errors.New("invalid Workspace deletion receipt digest")
			}
		}
		if receipt.ExpiresAt.IsZero() {
			return nil, errors.New("Workspace deletion receipt expiry required")
		}
		if audit := receipt.Audit; audit != nil {
			if !validWorkspaceDeletionAudit(*audit) || !audit.CommittedAt.Add(deletionReceiptTTL).Equal(receipt.ExpiresAt) {
				return nil, errors.New("invalid Workspace deletion audit")
			}
			raw, _ := json.Marshal(struct{ Slug, ETag string }{audit.Workspace, audit.ETag})
			digest := sha256.Sum256(raw)
			if hex.EncodeToString(digest[:]) != receipt.Intent {
				return nil, errors.New("Workspace deletion audit contradicts intent")
			}
		}
	}
	return dto.Deletions, nil
}

// DeleteWorkspaceChecked serializes exact lifetime/revision admission and replay
// before any independently durable revocation. Scope must be supplied by trusted
// application admission, never inferred from the caller's idempotency key.
// authorize runs on both fresh apply and replay under the routing lock; callbacks
// must not reenter this Store. beforeCommit is skipped for matching replay.
func (s *Store) DeleteWorkspaceChecked(ctx context.Context, slug routing.WorkspaceSlug, etag, scope, key string, authorize func(context.Context) error, beforeCommit func(routing.Config, routing.Config) error) (bool, error) {
	if s == nil {
		return false, ErrStoreClosed
	}
	if etag == "" || scope == "" || key == "" || authorize == nil {
		return false, errors.New("deletion requires ETag, trusted scope, idempotency key and admission")
	}
	intentBytes, _ := json.Marshal(struct{ Slug, ETag string }{slug.String(), etag})
	intentSum := sha256.Sum256(intentBytes)
	keyBytes, _ := json.Marshal(struct{ Scope, Key string }{scope, key})
	keySum := sha256.Sum256(keyBytes)
	intent := hex.EncodeToString(intentSum[:])
	keyHash := hex.EncodeToString(keySum[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == storeClosed {
		return false, ErrStoreClosed
	}

	if s.readOnly {
		return false, ErrReadOnly
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := authorize(ctx); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	now := time.Now().UTC()
	if receipt, ok := s.deletions[keyHash]; ok && now.Before(receipt.ExpiresAt) {
		if receipt.Intent != intent {
			return false, ErrIdempotencyConflict
		}
		return true, nil
	}
	workspace, ok := s.snapshot.Load().Workspace(slug)
	if !ok {
		return false, routing.ErrNotFound
	}
	if fmt.Sprintf("%s-%d", workspace.ManagementIncarnation(), workspace.ManagementRevision()) != etag {
		return false, ErrStaleRevision
	}
	receipts := make(map[string]deletionReceipt, len(s.deletions)+1)
	for k, v := range s.deletions {
		if now.Before(v.ExpiresAt) {
			receipts[k] = v
		}
	}
	if len(receipts) >= deletionReceiptCapacity {
		var oldest string
		for k, v := range receipts {
			if oldest == "" || v.ExpiresAt.Before(receipts[oldest].ExpiresAt) || (v.ExpiresAt.Equal(receipts[oldest].ExpiresAt) && k < oldest) {
				oldest = k
			}
		}
		delete(receipts, oldest)
	}
	receipts[keyHash] = deletionReceipt{Intent: intent, ExpiresAt: now.Add(deletionReceiptTTL), Audit: &WorkspaceDeletionAudit{Action: "workspace.delete", Caller: scope, Workspace: slug.String(), ETag: etag, CommittedAt: now}}
	_, err := s.updateLocked(ctx, func(c routing.Config) (routing.Config, error) { return c.DeleteWorkspace(slug) }, beforeCommit, receipts, s.creations)
	return false, err
}
