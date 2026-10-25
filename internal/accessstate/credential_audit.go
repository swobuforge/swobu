package accessstate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"time"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

// CredentialRevocationAudit retains reviewed public credential metadata and
// committed removal intent. It contains neither credential digest nor secret.
type CredentialRevocationAudit struct {
	Principal   string    `json:"principal"`
	Credential  string    `json:"credential"`
	Workspace   string    `json:"workspace"`
	CreatedAt   time.Time `json:"created_at"`
	Caller      string    `json:"caller"`
	Revision    string    `json:"revision"`
	CommittedAt time.Time `json:"committed_at"`
}

func validateCredentialRevocationAudits(principal string, receipts map[string]string, audits map[string]CredentialRevocationAudit) error {
	for key, audit := range audits {
		credential, err := ParseCredentialID(audit.Credential)
		if err != nil || credential.String() != audit.Credential || audit.Principal != principal || !validAuditCaller(audit.Caller) || audit.Revision == "" || audit.CreatedAt.IsZero() || audit.CommittedAt.IsZero() {
			return errors.New("invalid credential revocation audit")
		}
		workspace, err := routing.ParseWorkspaceSlug(audit.Workspace)
		if err != nil || workspace.String() != audit.Workspace {
			return errors.New("invalid credential revocation audit Workspace")
		}
		resource := "principals/" + principal + "/credentials/" + audit.Credential
		digest := sha256.Sum256([]byte("DELETE\x00" + resource + "\x00" + audit.Revision))
		if receipts[key] != hex.EncodeToString(digest[:]) {
			return errors.New("credential revocation audit does not match receipt intent")
		}
		identity, err := accessidentity.Parse(principal)
		if err != nil {
			return err
		}
		frozen := Access{ID: identity, Credentials: []Credential{{ID: credential, Workspace: workspace, CreatedAt: audit.CreatedAt}}}
		if credentialRevision(frozen, credential) != audit.Revision {
			return errors.New("credential revocation audit does not match reviewed metadata")
		}
	}
	return nil
}

// CredentialRevocationAudits returns detached committed public metadata for
// trusted local inspection, without backfilling historical retry receipts.
func (s *Store) CredentialRevocationAudits() []CredentialRevocationAudit {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []CredentialRevocationAudit{}
	for _, identity := range s.state {
		for _, audit := range identity.CredentialRevocationAudits {
			result = append(result, audit)
		}
	}
	sort.Slice(result, func(left, right int) bool {
		if !result[left].CommittedAt.Equal(result[right].CommittedAt) {
			return result[left].CommittedAt.Before(result[right].CommittedAt)
		}
		return result[left].Credential < result[right].Credential
	})
	return result
}
