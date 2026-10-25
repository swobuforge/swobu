package accessstate

import "time"

// CredentialIssuanceAudit retains committed public metadata, not credential
// secrets, delivery acknowledgement or current authority. Legacy facts stay absent.
type CredentialIssuanceAudit struct {
	Caller      string    `json:"caller"`
	Principal   string    `json:"principal"`
	Workspace   string    `json:"workspace"`
	Credential  string    `json:"credential"`
	CreatedAt   time.Time `json:"created_at"`
	CommittedAt time.Time `json:"committed_at"`
}

// CredentialIssuanceAudits returns detached explicitly captured checked issuance
// facts, without inferring commit history from old retry receipts or active keys.
func (s *Store) CredentialIssuanceAudits() []CredentialIssuanceAudit {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []CredentialIssuanceAudit{}
	for _, receipt := range s.credentialReceipts {
		if !receipt.CommittedAt.IsZero() {
			result = append(result, CredentialIssuanceAudit{Caller: receipt.Caller, Principal: receipt.Principal, Workspace: receipt.Workspace, Credential: receipt.Credential, CreatedAt: receipt.CreatedAt, CommittedAt: receipt.CommittedAt})
		}
	}
	return result
}
