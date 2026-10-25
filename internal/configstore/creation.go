package configstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/swobuforge/swobu/internal/routing"
	"gopkg.in/yaml.v3"
)

// Creation uses the existing local routing replay envelope: 4,096 entries and
// 24 hours. Results are original committed snapshots, not current-state reads.
const creationReceiptCapacity = deletionReceiptCapacity
const creationReceiptTTL = deletionReceiptTTL

type creationReceipt struct {
	Intent    string                  `yaml:"intent"`
	ExpiresAt time.Time               `yaml:"expires_at"`
	Result    string                  `yaml:"result"`
	Audit     *WorkspaceCreationAudit `yaml:"audit,omitempty"`
}

// WorkspaceCreationAudit retains original checked creation attribution beyond
// the retry envelope. It is historical fact, not present readiness or approval.
type WorkspaceCreationAudit struct {
	Action      string    `yaml:"action" json:"action"`
	Caller      string    `yaml:"caller" json:"caller"`
	Workspace   string    `yaml:"workspace" json:"workspace"`
	Incarnation string    `yaml:"incarnation" json:"incarnation"`
	CommittedAt time.Time `yaml:"committed_at" json:"committed_at"`
}

func creationResult(raw string) (routing.Workspace, error) {
	// Result documents contain only one Workspace, never nested retry state.
	var dto documentDTO
	if err := yaml.Unmarshal([]byte(raw), &dto); err != nil {
		return routing.Workspace{}, err
	}
	if len(dto.Creations) != 0 || len(dto.Deletions) != 0 || len(dto.CreationAudits) != 0 || len(dto.DeletionAudits) != 0 || len(dto.DisplayNameAudits) != 0 || len(dto.Workspaces) != 1 {
		return routing.Workspace{}, errors.New("invalid creation result envelope")
	}
	for _, w := range dto.Workspaces {
		if w.ManagementIncarnation == "" || w.ManagementRevision == nil || *w.ManagementRevision != 1 {
			return routing.Workspace{}, errors.New("creation result requires explicit committed lifetime")
		}
	}
	config, err := decode([]byte(raw))
	if err != nil {
		return routing.Workspace{}, err
	}
	w := config.Workspaces()[0]
	if w.ManagementIncarnation() == "" || w.ManagementRevision() != 1 {
		return routing.Workspace{}, errors.New("invalid creation result lifetime")
	}
	return w, nil
}

func decodeCreationReceipts(raw []byte) (map[string]creationReceipt, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	if len(document.Content) == 1 {
		root := document.Content[0]
		for index := 0; index+1 < len(root.Content); index += 2 {
			if root.Content[index].Value != "workspace_creations" {
				continue
			}
			receipts := root.Content[index+1]
			for entry := 1; entry < len(receipts.Content); entry += 2 {
				receipt := receipts.Content[entry]
				for field := 0; field+1 < len(receipt.Content); field += 2 {
					if receipt.Content[field].Value == "audit" && receipt.Content[field+1].Tag == "!!null" {
						return nil, errors.New("explicit null Workspace creation audit")
					}
				}
			}
		}
	}
	var dto documentDTO
	if err := yaml.Unmarshal(raw, &dto); err != nil {
		return nil, err
	}
	if len(dto.Creations) > creationReceiptCapacity {
		return nil, errors.New("too many Workspace creation receipts")
	}
	for key, receipt := range dto.Creations {
		for _, value := range []string{key, receipt.Intent} {
			digest, err := hex.DecodeString(value)
			if err != nil || len(digest) != sha256.Size || hex.EncodeToString(digest) != value {
				return nil, errors.New("invalid Workspace creation receipt digest")
			}
		}
		if receipt.ExpiresAt.IsZero() {
			return nil, errors.New("Workspace creation receipt expiry required")
		}
		workspace, err := creationResult(receipt.Result)
		if err != nil {
			return nil, err
		}
		if audit := receipt.Audit; audit != nil {
			if audit.Action != "workspace.create" || audit.Caller == "" || audit.Workspace != workspace.Slug().String() || audit.Incarnation != workspace.ManagementIncarnation() || audit.CommittedAt.IsZero() || !audit.CommittedAt.Add(creationReceiptTTL).Equal(receipt.ExpiresAt) {
				return nil, errors.New("invalid Workspace creation audit")
			}
		}
	}
	return dto.Creations, nil
}

// WorkspaceCreationAudits returns detached retained checked facts, including
// removed Workspaces. Retry expiry/eviction does not remove captured history.
func (s *Store) WorkspaceCreationAudits() ([]WorkspaceCreationAudit, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == storeClosed {
		return nil, ErrStoreClosed
	}
	result := []WorkspaceCreationAudit{}
	for _, audit := range s.creationAudits {
		result = append(result, audit)
	}
	return result, nil
}

// CreateWorkspaceChecked binds a validated draft to trusted caller scope and a
// retry key. Admission repeats under publication lock on apply and replay.
// Routing and original-result receipt commit together; callbacks cannot reenter
// this Store. A replay returns the original lifetime even after deletion.
func (s *Store) CreateWorkspaceChecked(ctx context.Context, draft routing.Workspace, scope, key string, authorize func(context.Context) error) (routing.Workspace, bool, error) {
	if s == nil {
		return routing.Workspace{}, false, ErrStoreClosed
	}
	if scope == "" || key == "" || authorize == nil {
		return routing.Workspace{}, false, errors.New("creation requires trusted scope, retry key and admission")
	}
	draft, canonical, err := CanonicalWorkspaceCreationDraft(draft)
	if err != nil {
		return routing.Workspace{}, false, err
	}
	intentSum := sha256.Sum256(canonical)
	scopeBytes, _ := json.Marshal(struct{ Scope, Method, Resource, Key string }{scope, "POST", "/api/v1/workspaces", key})
	keySum := sha256.Sum256(scopeBytes)
	intent, keyHash := hex.EncodeToString(intentSum[:]), hex.EncodeToString(keySum[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == storeClosed {
		return routing.Workspace{}, false, ErrStoreClosed
	}

	if s.readOnly {
		return routing.Workspace{}, false, ErrReadOnly
	}
	if err := ctx.Err(); err != nil {
		return routing.Workspace{}, false, err
	}
	if err := authorize(ctx); err != nil {
		return routing.Workspace{}, false, err
	}
	if err := ctx.Err(); err != nil {
		return routing.Workspace{}, false, err
	}
	now := time.Now().UTC()
	if receipt, exists := s.creations[keyHash]; exists && now.Before(receipt.ExpiresAt) {
		if receipt.Intent != intent {
			return routing.Workspace{}, false, ErrIdempotencyConflict
		}
		original, err := creationResult(receipt.Result)
		return original, true, err
	}
	if err := s.checkCreationAvailability(draft); err != nil {
		return routing.Workspace{}, false, err
	}
	current := s.snapshot.Load().Clone()
	next, err := routing.NewConfig(append(current.Workspaces(), draft))
	if err != nil {
		return routing.Workspace{}, false, err
	}
	next, err = stampWorkspaceRevisions(current, next)
	if err != nil {
		return routing.Workspace{}, false, err
	}
	committed, _ := next.Workspace(draft.Slug())
	resultConfig, err := routing.NewConfig([]routing.Workspace{committed})
	if err != nil {
		return routing.Workspace{}, false, err
	}
	result, err := encodeDocument(resultConfig, routing.ReferenceSchemaVersion)
	if err != nil {
		return routing.Workspace{}, false, err
	}
	receipts := make(map[string]creationReceipt, len(s.creations)+1)
	for k, v := range s.creations {
		if now.Before(v.ExpiresAt) {
			receipts[k] = v
		}
	}
	if len(receipts) >= creationReceiptCapacity {
		var oldest string
		for k, v := range receipts {
			if oldest == "" || v.ExpiresAt.Before(receipts[oldest].ExpiresAt) || (v.ExpiresAt.Equal(receipts[oldest].ExpiresAt) && k < oldest) {
				oldest = k
			}
		}
		delete(receipts, oldest)
	}
	receipts[keyHash] = creationReceipt{Intent: intent, ExpiresAt: now.Add(creationReceiptTTL), Result: string(result), Audit: &WorkspaceCreationAudit{Action: "workspace.create", Caller: scope, Workspace: committed.Slug().String(), Incarnation: committed.ManagementIncarnation(), CommittedAt: now}}
	// Preserve the stamped candidate: publication must not mint a second lifetime
	// after the result has been bound into its receipt.
	if err := ctx.Err(); err != nil {
		return routing.Workspace{}, false, err
	}
	_, err = s.publishLocked(current, next, nil, s.deletions, receipts, s.displayNameAudits)
	if err != nil {
		return routing.Workspace{}, false, err
	}
	return committed, false, nil
}
