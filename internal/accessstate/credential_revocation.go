package accessstate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

var ErrCredentialRevocationIdempotencyConflict = errors.New("idempotency key conflicts with a different credential revocation")

// CredentialRevocationPreview is detached public impact, never credential material.
// Revision binds the immutable credential and Principal revocation state; labels,
// sibling credentials and permission changes do not change the removal's scope.
type CredentialRevocationPreview struct {
	Principal  accessidentity.ID
	Credential CredentialID
	Workspace  routing.WorkspaceSlug
	CreatedAt  time.Time
	Revision   string
}

func credentialRevision(identity Access, id CredentialID) string {
	impact := struct {
		Principal  string
		Credential string
		RevokedAt  *time.Time
		Workspace  string
		CreatedAt  time.Time
	}{Principal: identity.ID.String(), Credential: id.String(), RevokedAt: identity.RevokedAt}
	for _, credential := range identity.Credentials {
		if credential.ID == id {
			impact.Workspace, impact.CreatedAt = credential.Workspace.String(), credential.CreatedAt
			break
		}
	}
	raw, _ := json.Marshal(impact)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// PreviewCredentialRevocation reads impact under the Access lock after trusted
// admission. It neither withdraws authority nor reserves an idempotency key.
// Callbacks must not reenter the store; preview is not approval.
func (s *Store) PreviewCredentialRevocation(principal accessidentity.ID, credential CredentialID, authorize func() error) (CredentialRevocationPreview, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if authorize == nil {
		return CredentialRevocationPreview{}, errors.New("credential preview requires trusted admission")
	}
	if err := authorize(); err != nil {
		return CredentialRevocationPreview{}, err
	}
	index, exists := s.byAccess[principal]
	if !exists {
		return CredentialRevocationPreview{}, ErrNotFound
	}
	for _, value := range s.state[index].Credentials {
		if value.ID == credential {
			return CredentialRevocationPreview{Principal: principal, Credential: credential, Workspace: value.Workspace, CreatedAt: value.CreatedAt, Revision: credentialRevision(s.state[index], credential)}, nil
		}
	}
	return CredentialRevocationPreview{}, ErrNotFound
}

// RevokeCredentialChecked is the internal revision/admission transaction. It
// removes only the reviewed credential, preserving permissions and siblings.
// Remote requests recheck credential revocation before each operation;
// this primitive must not be used as an unqualified Control retry contract.
func (s *Store) RevokeCredentialChecked(principal accessidentity.ID, credential CredentialID, expected string, authorize func() error) (Access, error) {
	if expected == "" || authorize == nil {
		return Access{}, errors.New("credential revocation requires revision and trusted admission")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := authorize(); err != nil {
		return Access{}, err
	}
	index, exists := s.byAccess[principal]
	if !exists {
		return Access{}, ErrNotFound
	}
	// Check stale consent before absence: a concurrently removed credential must
	// not make an old reviewed request look like an approved no-op.
	if credentialRevision(s.state[index], credential) != expected {
		return Access{}, ErrRevisionConflict
	}
	candidate := cloneState(s.state)
	for position, value := range candidate[index].Credentials {
		if value.ID != credential {
			continue
		}
		candidate[index].Credentials = append(candidate[index].Credentials[:position], candidate[index].Credentials[position+1:]...)
		if err := s.commit(candidate); err != nil {
			return Access{}, err
		}
		return cloneAccess(s.state[s.byAccess[principal]]), nil
	}
	return Access{}, ErrNotFound
}

// ApplyCredentialRevocation retains local caller/method/resource/revision-bound
// digests with the selected credential's removal in one authority publication.
// Exact replay repeats admission and returns current detached Principal metadata,
// never a refreshed consent or another effect. Callbacks must not reenter Access.
func (s *Store) ApplyCredentialRevocation(ctx context.Context, principal accessidentity.ID, credential CredentialID, expected, key string, authorize func() error) (Access, bool, error) {
	if expected == "" || authorize == nil || len(key) == 0 || len(key) > 256 || strings.ContainsAny(key, " \t\r\n") {
		return Access{}, false, errors.New("credential revocation requires revision, trusted admission and one retry key")
	}
	resource := "principals/" + principal.String() + "/credentials/" + credential.String()
	keySum := sha256.Sum256([]byte(accessidentity.Attribution(ctx).String() + "\x00DELETE\x00" + resource + "\x00" + key))
	intentSum := sha256.Sum256([]byte("DELETE\x00" + resource + "\x00" + expected))
	keyHash, intentHash := hex.EncodeToString(keySum[:]), hex.EncodeToString(intentSum[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := authorize(); err != nil {
		return Access{}, false, err
	}
	index, exists := s.byAccess[principal]
	if !exists {
		return Access{}, false, ErrNotFound
	}
	if original, exists := s.state[index].CredentialRevocationReceipts[keyHash]; exists {
		if original != intentHash {
			return Access{}, false, ErrCredentialRevocationIdempotencyConflict
		}
		return cloneAccess(s.state[index]), true, nil
	}
	if credentialRevision(s.state[index], credential) != expected {
		return Access{}, false, ErrRevisionConflict
	}
	candidate := cloneState(s.state)
	for position, value := range candidate[index].Credentials {
		if value.ID != credential {
			continue
		}
		candidate[index].Credentials = append(candidate[index].Credentials[:position], candidate[index].Credentials[position+1:]...)
		if candidate[index].CredentialRevocationReceipts == nil {
			candidate[index].CredentialRevocationReceipts = make(map[string]string)
		}
		// TODO: qualify a retention envelope before eviction; forgetting an
		// uncertain original intent changes the promised recovery outcome.
		candidate[index].CredentialRevocationReceipts[keyHash] = intentHash
		if candidate[index].CredentialRevocationAudits == nil {
			candidate[index].CredentialRevocationAudits = make(map[string]CredentialRevocationAudit)
		}
		candidate[index].CredentialRevocationAudits[keyHash] = CredentialRevocationAudit{Principal: principal.String(), Credential: credential.String(), Workspace: value.Workspace.String(), CreatedAt: value.CreatedAt, Caller: accessidentity.Attribution(ctx).String(), Revision: expected, CommittedAt: s.now().UTC()}
		if err := s.commit(candidate); err != nil {
			return Access{}, false, err
		}
		return cloneAccess(s.state[s.byAccess[principal]]), false, nil
	}
	return Access{}, false, ErrNotFound
}
