package accessstate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

var ErrCredentialIdempotencyConflict = errors.New("idempotency key conflicts with a different credential issuance")

// CredentialIssuance reads public receipt metadata only, including after
// revocation. An absent receipt is not evidence that an in-flight writer will
// never publish; the caller's retained Connect attempt still forbids retry.
func (s *Store) CredentialIssuance(principal, workspace, key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keySum := sha256.Sum256([]byte("local\x00credential-issuance\x00" + key))
	for _, receipt := range s.credentialReceipts {
		if receipt.KeyHash == hex.EncodeToString(keySum[:]) {
			if receipt.Principal != principal || receipt.Workspace != workspace {
				return "", ErrCredentialIdempotencyConflict
			}
			return receipt.Credential, nil
		}
	}
	return "", nil
}

// CredentialReceipt retains only issuance attribution and public metadata, even
// after credential revocation. It is transaction evidence, not active authority.
// Receipts are not evicted: forgetting a key would permit duplicate issuance.
type CredentialReceipt struct {
	Caller      string    `json:"caller"`
	KeyHash     string    `json:"key_hash"`
	IntentHash  string    `json:"intent_hash"`
	Principal   string    `json:"principal"`
	Workspace   string    `json:"workspace"`
	Credential  string    `json:"credential"`
	CreatedAt   time.Time `json:"created_at"`
	CommittedAt time.Time `json:"committed_at,omitzero"`
}

func validateCredentialReceipts(receipts []CredentialReceipt) error {
	seen := make(map[string]bool)
	for _, receipt := range receipts {
		if !receipt.CommittedAt.IsZero() {
			digest := sha256.Sum256([]byte("POST\x00principals/" + receipt.Principal + "/credentials\x00" + receipt.Workspace))
			if receipt.IntentHash != hex.EncodeToString(digest[:]) {
				return errors.New("credential issuance audit does not match intent")
			}
		}
		if receipt.Caller != accessidentity.Local.String() || receipt.CreatedAt.IsZero() {
			return errors.New("invalid credential receipt attribution")
		}
		if _, err := accessidentity.RestoreManaged(receipt.Principal); err != nil {
			return err
		}
		if _, err := routing.ParseWorkspaceSlug(receipt.Workspace); err != nil {
			return err
		}
		if _, err := ParseCredentialID(receipt.Credential); err != nil {
			return err
		}
		for _, value := range []string{receipt.KeyHash, receipt.IntentHash} {
			decoded, err := hex.DecodeString(value)
			if err != nil || len(decoded) != sha256.Size || value != strings.ToLower(value) {
				return errors.New("invalid credential receipt digest")
			}
		}
		if seen[receipt.KeyHash] {
			return errors.New("duplicate credential receipt key")
		}
		seen[receipt.KeyHash] = true
	}
	return nil
}

// IssueCredentialChecked publishes receipt and credential atomically. Admission
// runs under the publication lock before replay and must not reenter this store.
// Replay returns original metadata without a digest or token, even after revoke;
// it never restores authority or claims the secret was delivered successfully.
func (s *Store) IssueCredentialChecked(id accessidentity.ID, workspace routing.WorkspaceSlug, key string, authorize func() error) (IssuedCredential, bool, error) {
	return s.issueCredentialChecked(id, workspace, key, authorize, "", "")
}

// IssueClientCredentialChecked retains the established issuance lifecycle while
// admitting the reviewed Models permission under the same publication lock.
func (s *Store) IssueClientCredentialChecked(id accessidentity.ID, workspace routing.WorkspaceSlug, key, permissionRevision string, authorize func() error) (IssuedCredential, bool, error) {
	if permissionRevision == "" {
		return IssuedCredential{}, false, ErrRevisionConflict
	}
	return s.issueCredentialChecked(id, workspace, key, authorize, permissionRevision, routing.ServiceModels)
}

func (s *Store) issueCredentialChecked(id accessidentity.ID, workspace routing.WorkspaceSlug, key string, authorize func() error, revision string, service routing.ServiceKind) (IssuedCredential, bool, error) {
	if authorize == nil || len(key) == 0 || len(key) > 256 || strings.ContainsAny(key, " \t\r\n") {
		return IssuedCredential{}, false, errors.New("credential issuance requires trusted admission and one retry key")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := authorize(); err != nil {
		return IssuedCredential{}, false, err
	}
	keySum := sha256.Sum256([]byte("local\x00credential-issuance\x00" + key))
	intentSum := sha256.Sum256([]byte("POST\x00principals/" + id.String() + "/credentials\x00" + workspace.String()))
	keyHash, intentHash := hex.EncodeToString(keySum[:]), hex.EncodeToString(intentSum[:])
	for _, receipt := range s.credentialReceipts {
		if receipt.KeyHash != keyHash {
			continue
		}
		if receipt.IntentHash != intentHash {
			return IssuedCredential{}, false, ErrCredentialIdempotencyConflict
		}
		credentialID, err := ParseCredentialID(receipt.Credential)
		if err != nil {
			return IssuedCredential{}, false, err
		}
		return IssuedCredential{Credential: Credential{ID: credentialID, Workspace: workspace, CreatedAt: receipt.CreatedAt}}, true, nil
	}
	index, ok := s.byAccess[id]
	if !ok {
		return IssuedCredential{}, false, ErrNotFound
	}
	if s.state[index].RevokedAt != nil {
		return IssuedCredential{}, false, ErrRevoked
	}
	if revision != "" && revision != permissionRevision(s.state[index], workspace) {
		return IssuedCredential{}, false, ErrRevisionConflict
	}
	if service != "" && !permissionAllowsService(s.state[index], workspace, service, s.now()) {
		return IssuedCredential{}, false, ErrUnauthorized
	}
	if !permissionAllows(s.state[index], workspace, s.now()) {
		return IssuedCredential{}, false, ErrUnauthorized
	}
	issued, err := s.newCredential(workspace)
	if err != nil {
		return IssuedCredential{}, false, err
	}
	candidate := cloneState(s.state)
	candidate[index].Credentials = append(candidate[index].Credentials, issued.Credential)
	receipts := append(append([]CredentialReceipt(nil), s.credentialReceipts...), CredentialReceipt{Caller: "local", KeyHash: keyHash, IntentHash: intentHash, Principal: id.String(), Workspace: workspace.String(), Credential: issued.Credential.ID.String(), CreatedAt: issued.Credential.CreatedAt, CommittedAt: s.now().UTC()})
	if err := s.commitWithReceipts(candidate, s.permissionReceipts, receipts); err != nil {
		return IssuedCredential{}, false, err
	}
	return issued, false, nil
}
