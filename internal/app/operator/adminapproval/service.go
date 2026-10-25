package adminapproval

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("admin approval not found")
var ErrInvalid = errors.New("admin approval is invalid or expired")
var ErrUsed = errors.New("admin approval already used")

type Approval struct {
	ID        string    `json:"id"`
	Workspace string    `json:"workspace"`
	Operation string    `json:"operation"`
	Digest    string    `json:"digest"`
	Revision  string    `json:"revision"`
	ExpiresAt time.Time `json:"expires_at"`
	Used      bool      `json:"used"`
}

type Service struct {
	mu        sync.Mutex
	now       func() time.Time
	approvals map[string]Approval
}

func New(now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{now: now, approvals: map[string]Approval{}}
}

func (s *Service) Create(workspace, operation, revision string, args any, ttl time.Duration) (Approval, error) {
	workspace, operation, revision = strings.TrimSpace(workspace), strings.TrimSpace(operation), strings.TrimSpace(revision)
	if workspace == "" || operation == "" || revision == "" {
		return Approval{}, ErrInvalid
	}
	if ttl <= 0 || ttl > 10*time.Minute {
		ttl = 2 * time.Minute
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return Approval{}, ErrInvalid
	}
	digest := sha256.Sum256(raw)
	id, err := randomID()
	if err != nil {
		return Approval{}, err
	}
	value := Approval{ID: id, Workspace: workspace, Operation: operation, Digest: base64.RawURLEncoding.EncodeToString(digest[:]), Revision: revision, ExpiresAt: s.now().UTC().Add(ttl)}
	s.mu.Lock()
	s.approvals[id] = value
	s.mu.Unlock()
	return value, nil
}

func (s *Service) Consume(id, workspace, operation, revision string, args any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.approvals[strings.TrimSpace(id)]
	if !ok {
		return ErrNotFound
	}
	if value.Used {
		return ErrUsed
	}
	if !s.now().UTC().Before(value.ExpiresAt) {
		return ErrInvalid
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return ErrInvalid
	}
	digest := sha256.Sum256(raw)
	if value.Workspace != strings.TrimSpace(workspace) || value.Operation != strings.TrimSpace(operation) || value.Revision != strings.TrimSpace(revision) || value.Digest != base64.RawURLEncoding.EncodeToString(digest[:]) {
		return ErrInvalid
	}
	value.Used = true
	s.approvals[value.ID] = value
	return nil
}

// Get returns a non-authorizing snapshot for an approval identifier. Applying
// a mutation must still call Consume immediately before its effect; callers
// cannot use Get as proof of approval.
func (s *Service) Get(id string) (Approval, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.approvals[strings.TrimSpace(id)]
	if !ok {
		return Approval{}, ErrNotFound
	}
	if !s.now().UTC().Before(value.ExpiresAt) && !value.Used {
		return Approval{}, ErrInvalid
	}
	return value, nil
}

func randomID() (string, error) {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "aga_" + base64.RawURLEncoding.EncodeToString(raw), nil
}
