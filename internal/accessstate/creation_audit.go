package accessstate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

// PrincipalCreationAudit retains the original checked creation label and caller,
// not current presentation or Workspace authority. Historical facts stay absent.
type PrincipalCreationAudit struct {
	Principal   string    `json:"principal"`
	Caller      string    `json:"caller"`
	Label       string    `json:"label"`
	CommittedAt time.Time `json:"committed_at"`
}

func validatePrincipalCreationAudit(identity Access, audit *PrincipalCreationAudit) error {
	if audit == nil {
		return nil
	}
	label, err := validateName(audit.Label)
	if err != nil || label != audit.Label || audit.Principal != identity.ID.String() || !validAuditCaller(audit.Caller) || audit.CommittedAt.IsZero() || !audit.CommittedAt.Equal(identity.CreatedAt) || identity.CreationKeyHash == "" {
		return errors.New("invalid Principal creation audit")
	}
	digest := sha256.Sum256([]byte("POST\x00principals\x00" + audit.Label))
	if identity.CreationIntentHash != hex.EncodeToString(digest[:]) {
		return errors.New("Principal creation audit does not match original intent")
	}
	return nil
}

// PrincipalCreationAudits returns detached original checked creation facts for
// private Node inspection. Current labels and historical receipts are not sources.
func (s *Store) PrincipalCreationAudits() []PrincipalCreationAudit {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []PrincipalCreationAudit{}
	for _, identity := range s.state {
		if identity.CreationAudit != nil {
			result = append(result, *identity.CreationAudit)
		}
	}
	return result
}
