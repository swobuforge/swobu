package connections

import (
	"errors"
	"time"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
)

// Audit is a checked committed management fact. It contains identity, actor
// and revisions only: no spec, label, endpoint, credential locator or retry key.
// Facts survive resource deletion and retry expiry. Unchecked imports/internal
// publications do not fabricate operator attribution.
type Audit struct {
	Action         string    `json:"action"`
	Caller         string    `json:"caller"`
	ConnectionID   string    `json:"connection_id"`
	BeforeRevision string    `json:"before_revision,omitempty"`
	AfterRevision  string    `json:"after_revision,omitempty"`
	CommittedAt    time.Time `json:"committed_at"`
}

// Validate checks the closed action/revision contract without consulting current
// resources. Transport consumers additionally bind these facts to their envelope.
func (a Audit) Validate() error {
	if a.Caller == "" || a.CommittedAt.IsZero() {
		return errors.New("invalid Connection audit attribution")
	}
	if _, err := connectionidentity.Parse(a.ConnectionID); err != nil {
		return errors.New("invalid Connection audit identity")
	}
	if a.Action == "connection.delete" {
		if _, err := ParseRevision(a.BeforeRevision); err != nil || a.AfterRevision != "" {
			return errors.New("invalid Connection deletion audit")
		}
		return nil
	}
	after, err := ParseRevision(a.AfterRevision)
	if err != nil {
		return errors.New("invalid Connection audit revision")
	}
	switch a.Action {
	case "connection.create":
		if a.BeforeRevision != "" || after.value != 1 {
			return errors.New("invalid Connection creation audit")
		}
	case "connection.edit", "connection.credential_replace":
		before, err := ParseRevision(a.BeforeRevision)
		if err != nil || before.value == ^uint64(0) || after.value != before.value+1 {
			return errors.New("invalid Connection edit audit")
		}
	default:
		return errors.New("invalid Connection audit action")
	}
	return nil
}

// Audits returns detached retained facts in commit order under one registry
// read lock. It is a private operator projection, not execution evidence.
func (s *Store) Audits() []Audit {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Audit{}, s.audits...)
}

func appendAudit(source []Audit, value Audit) []Audit {
	result := make([]Audit, len(source), len(source)+1)
	copy(result, source)
	return append(result, value)
}
