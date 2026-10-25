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

// PrincipalRevocationAudit retains checked intent and affected scope captured
// before publication. Empty scope is valid; historical receipts remain unaudited.
type PrincipalRevocationAudit struct {
	Principal   string    `json:"principal"`
	Caller      string    `json:"caller"`
	Revision    string    `json:"revision"`
	Workspaces  []string  `json:"workspaces"`
	CommittedAt time.Time `json:"committed_at"`
}

func validatePrincipalRevocationAudits(principal string, receipts map[string]string, audits map[string]PrincipalRevocationAudit) error {
	for key, audit := range audits {
		if audit.Principal != principal || audit.Caller != accessidentity.Local.String() || audit.Revision == "" || audit.CommittedAt.IsZero() || audit.Workspaces == nil {
			return errors.New("invalid Principal revocation audit")
		}
		digest := sha256.Sum256([]byte("DELETE\x00principals/" + principal + "\x00" + audit.Revision))
		if receipts[key] != hex.EncodeToString(digest[:]) {
			return errors.New("Principal revocation audit does not match receipt intent")
		}
		for index, workspace := range audit.Workspaces {
			slug, err := routing.ParseWorkspaceSlug(workspace)
			if err != nil || slug.String() != workspace || (index > 0 && audit.Workspaces[index-1] >= workspace) {
				return errors.New("invalid Principal revocation audit scope")
			}
		}
	}
	return nil
}

func principalRevocationAudit(identity Access, revision string, committed time.Time) PrincipalRevocationAudit {
	scope := make(map[string]bool)
	for _, permission := range identity.Permissions {
		scope[permission.Workspace.String()] = true
	}
	for _, credential := range identity.Credentials {
		scope[credential.Workspace.String()] = true
	}
	workspaces := make([]string, 0, len(scope))
	for workspace := range scope {
		workspaces = append(workspaces, workspace)
	}
	sort.Strings(workspaces)
	return PrincipalRevocationAudit{Principal: identity.ID.String(), Caller: accessidentity.Local.String(), Revision: revision, Workspaces: workspaces, CommittedAt: committed.UTC()}
}

// PrincipalRevocationAudits returns detached committed facts without retry hashes
// for trusted local inspection. It neither backfills history nor grants authority.
func (s *Store) PrincipalRevocationAudits() []PrincipalRevocationAudit {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []PrincipalRevocationAudit{}
	for _, identity := range s.state {
		for _, audit := range identity.RevocationAudits {
			audit.Workspaces = append([]string{}, audit.Workspaces...)
			result = append(result, audit)
		}
	}
	sort.Slice(result, func(left, right int) bool {
		if !result[left].CommittedAt.Equal(result[right].CommittedAt) {
			return result[left].CommittedAt.Before(result[right].CommittedAt)
		}
		if result[left].Principal != result[right].Principal {
			return result[left].Principal < result[right].Principal
		}
		return result[left].Revision < result[right].Revision
	})
	return result
}
