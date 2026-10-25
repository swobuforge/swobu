// Package publications owns the set of Workspaces reachable through the relay.
// Publication changes reachability only; Workspace credentials, Services and
// limits remain the sole authority for data operations.
package publications

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/swobuforge/swobu/internal/routing"
)

var ErrConflict = errors.New("publication changed; reload and retry")

type document struct {
	Workspaces []string `json:"workspaces"`
}

// Store projects one Node-owned publication set. The writer must atomically
// replace the complete Node document; failed writes leave this view unchanged.
// Lifecycle locking orders relay readiness and Workspace deletion before Set.
type publishedMembership struct{ workspaces []string }

type Store struct {
	membership atomic.Pointer[publishedMembership]
	lifecycle  sync.Mutex
	mu         sync.Mutex
	workspaces []string
	publish    func([]byte) error
}

// ValidateDocument accepts only the current sorted, unique Workspace set.
// Absent state is empty; obsolete authorization and exposure fields are errors.
func ValidateDocument(raw []byte) ([]routing.WorkspaceSlug, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var d document
	if err := dec.Decode(&d); err != nil {
		return nil, fmt.Errorf("publication document: %w", err)
	}
	if dec.Decode(&struct{}{}) != io.EOF || d.Workspaces == nil {
		return nil, errors.New("publication document requires one nonnull workspaces array")
	}
	result := make([]routing.WorkspaceSlug, 0, len(d.Workspaces))
	previous := ""
	for _, rawSlug := range d.Workspaces {
		slug, err := routing.ParseWorkspaceSlug(rawSlug)
		if err != nil || slug.String() != rawSlug {
			return nil, fmt.Errorf("invalid published Workspace %q", rawSlug)
		}
		if previous != "" && previous >= rawSlug {
			return nil, errors.New("published Workspaces must be sorted and unique")
		}
		result = append(result, slug)
		previous = rawSlug
	}
	return result, nil
}

// OpenInline opens a publication view backed by the owning Node writer.
// It neither creates a sidecar nor translates historical documents.
func OpenInline(raw []byte, publish func([]byte) error) (*Store, error) {
	if publish == nil {
		return nil, errors.New("publication requires Node writer")
	}
	parsed, err := ValidateDocument(raw)
	if err != nil {
		return nil, err
	}
	s := &Store{publish: publish, workspaces: []string{}}
	for _, x := range parsed {
		s.workspaces = append(s.workspaces, x.String())
	}
	s.membership.Store(&publishedMembership{append([]string{}, s.workspaces...)})
	return s, nil
}

// LockLifecycle serializes publication readiness with Workspace destruction.
func (s *Store) LockLifecycle() func() { s.lifecycle.Lock(); return s.lifecycle.Unlock }
func (s *Store) Has(workspace routing.WorkspaceSlug) bool {
	snapshot := s.membership.Load()
	return snapshot != nil && slices.Contains(snapshot.workspaces, workspace.String())
}
func (s *Store) HasPublicWorkspace() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.workspaces) > 0
}
func (s *Store) List() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.workspaces...)
}
func (s *Store) Revision() string { s.mu.Lock(); defer s.mu.Unlock(); return revision(s.workspaces) }
func revision(workspaces []string) string {
	raw, _ := json.Marshal(document{Workspaces: workspaces})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// WithReviewSnapshot projects membership and its revision under one lock.
// The callback must not reenter this store or retain mutable state.
func (s *Store) WithReviewSnapshot(project func(string, []string) error) error {
	if project == nil {
		return errors.New("publication reviewer required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return project(revision(s.workspaces), append([]string{}, s.workspaces...))
}

// Set is the entire publication mutation: a revision-checked update of one
// membership bit, in the same Node document as every other control owner.
// The caller must hold LockLifecycle and check relay readiness for enabling.
func (s *Store) Set(workspace routing.WorkspaceSlug, enabled bool, expected string, authorize func() error) error {
	if authorize == nil {
		return errors.New("local Node admission required")
	}
	if _, err := routing.ParseWorkspaceSlug(workspace.String()); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := authorize(); err != nil {
		return err
	}
	if expected != revision(s.workspaces) {
		return ErrConflict
	}
	next := append([]string{}, s.workspaces...)
	index, found := slices.BinarySearch(next, workspace.String())
	if enabled && !found {
		next = slices.Insert(next, index, workspace.String())
	}
	if !enabled && found {
		next = slices.Delete(next, index, index+1)
	}
	if slices.Equal(next, s.workspaces) {
		return nil
	}
	raw, err := json.Marshal(document{Workspaces: next})
	if err != nil {
		return err
	}
	if err = s.publish(raw); err != nil {
		return err
	}
	s.workspaces = next
	s.membership.Store(&publishedMembership{append([]string{}, next...)})
	return nil
}
