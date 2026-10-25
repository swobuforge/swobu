package accessstate

import (
	"errors"
	"time"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
)

// PrincipalLabelAudit retains original changed-label intent, not current
// presentation or Workspace authority. Generation binds the reviewed revision.
type PrincipalLabelAudit struct {
	Principal   string    `json:"principal"`
	Caller      string    `json:"caller"`
	Before      string    `json:"before"`
	After       string    `json:"after"`
	Revision    string    `json:"revision"`
	Generation  uint64    `json:"generation"`
	CommittedAt time.Time `json:"committed_at"`
}

func validatePrincipalLabelAudits(principal string, generation uint64, audits []PrincipalLabelAudit) error {
	id, err := accessidentity.RestoreManaged(principal)
	if err != nil {
		return err
	}
	for index, audit := range audits {
		before, beforeErr := validateName(audit.Before)
		after, afterErr := validateName(audit.After)
		if audit.Principal != principal || !validAuditCaller(audit.Caller) || beforeErr != nil || afterErr != nil || before != audit.Before || after != audit.After || before == after || audit.CommittedAt.IsZero() || audit.Generation >= generation || audit.Revision != PrincipalRevision(Access{ID: id, AuthorityGeneration: audit.Generation}) || (index > 0 && audits[index-1].Generation >= audit.Generation) {
			return errors.New("invalid Principal label audit")
		}
	}
	return nil
}

// PrincipalLabelAudits returns detached committed checked edits for private Node
// inspection. No-op calls and historical unchecked changes create no facts.
func (s *Store) PrincipalLabelAudits() []PrincipalLabelAudit {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []PrincipalLabelAudit{}
	for _, identity := range s.state {
		result = append(result, identity.LabelAudits...)
	}
	return result
}
