// Package credentialmaterialization owns daemon-created immutable references
// for pasted secret material and their restart-safe pending lifecycle.
package credentialmaterialization

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/platform/atomicfile"
)

type Store func(providerSpec, materialID, secret string) (string, error)
type Reclaim func(providerSpec, credentialRef string) error

type Record struct {
	Provider string    `json:"provider"`
	Ref      string    `json:"ref"`
	State    string    `json:"state"`
	Expires  time.Time `json:"expires_at"`
}

type registry struct {
	SchemaVersion int      `json:"schema_version"`
	Records       []Record `json:"records"`
}

const (
	statePending   = "pending"
	stateCommitted = "committed"
	stateRetired   = "retired"
)

type Service struct {
	store   Store
	reclaim Reclaim
	path    string
	mu      sync.Mutex
	pending map[string]Record
}

func NewService(store Store, reclaim ...Reclaim) (Service, error) {
	if store == nil {
		return Service{}, fmt.Errorf("credential material store is required")
	}
	var cleanup Reclaim
	if len(reclaim) > 0 {
		cleanup = reclaim[0]
	}
	return Service{store: store, reclaim: cleanup, pending: map[string]Record{}}, nil
}

// SetPendingPath loads the private metadata index. It contains no secret values.
func (s *Service) SetPendingPath(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.path = strings.TrimSpace(path)
	if s.path == "" {
		return nil
	}
	raw, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var document registry
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return fmt.Errorf("decode pending credential metadata: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return fmt.Errorf("decode credential material registry: trailing data")
	} else if err != io.EOF {
		return fmt.Errorf("decode credential material registry: trailing data: %w", err)
	}
	if document.SchemaVersion != 1 {
		return fmt.Errorf("unsupported credential material registry schema version %d", document.SchemaVersion)
	}
	if document.Records == nil {
		return fmt.Errorf("credential material registry records must be an array")
	}
	loaded := make(map[string]Record, len(document.Records))
	for _, entry := range document.Records {
		if err := validateEntry(entry); err != nil {
			return err
		}
		if _, exists := loaded[entry.Ref]; exists {
			return fmt.Errorf("duplicate credential material registry reference %q", entry.Ref)
		}
		loaded[entry.Ref] = entry
	}
	s.pending = loaded
	return nil
}

// Materialize writes the secret and persists its pending lease before returning.
func (s *Service) Materialize(_ context.Context, providerSpec, secret string) (string, error) {
	providerSpec = strings.TrimSpace(providerSpec)
	secret = strings.TrimSpace(secret)
	if providerSpec == "" || secret == "" {
		return "", fmt.Errorf("provider spec and secret are required")
	}
	materialID, err := newMaterialID()
	if err != nil {
		return "", err
	}
	ref, err := s.store(providerSpec, materialID, secret)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Errorf("credential material store returned an empty reference")
	}
	entry := Record{Provider: providerSpec, Ref: strings.TrimSpace(ref), State: statePending, Expires: time.Now().Add(24 * time.Hour)}
	s.mu.Lock()
	s.pending[entry.Ref] = entry
	err = s.persistLocked()
	if err != nil {
		delete(s.pending, entry.Ref)
	}
	s.mu.Unlock()
	if err != nil {
		if s.reclaim != nil {
			if reclaimErr := s.reclaim(providerSpec, entry.Ref); reclaimErr != nil {
				slog.Warn("credential material cleanup deferred after metadata failure", "event", "credential_material_cleanup_deferred", "provider", providerSpec, "error", reclaimErr)
			}
		}
		return "", fmt.Errorf("persist pending credential metadata: %w", err)
	}
	return entry.Ref, nil
}

// Reconcile removes committed pending entries and reclaims expired unreferenced entries.
func (s *Service) Reconcile(referenced map[string]struct{}, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for ref, entry := range s.pending {
		if _, ok := referenced[ref]; ok {
			if entry.State != stateCommitted || !entry.Expires.IsZero() {
				entry.State = stateCommitted
				entry.Expires = time.Time{}
				s.pending[ref] = entry
				changed = true
			}
			continue
		}
		if entry.State == statePending && now.Before(entry.Expires) {
			continue
		}
		if s.reclaim != nil {
			if err := s.reclaim(entry.Provider, entry.Ref); err != nil {
				entry.State = stateRetired
				entry.Expires = time.Time{}
				s.pending[ref] = entry
				slog.Warn("credential material reclamation deferred", "event", "credential_material_reclaim_deferred", "provider", entry.Provider, "ref", entry.Ref, "state", stateRetired, "error", err)
				changed = true
				continue
			}
		}
		delete(s.pending, ref)
		changed = true
	}
	if changed {
		return s.persistLocked()
	}
	return nil
}

// Retire reclaims daemon-created refs absent from the post-mutation canonical set.
func (s *Service) Retire(before, after map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = before
	for ref, entry := range s.pending {
		if _, existed := before[ref]; !existed {
			continue
		}
		if _, ok := after[ref]; ok {
			continue
		}
		if s.reclaim != nil {
			if err := s.reclaim(entry.Provider, ref); err != nil {
				entry.State = stateRetired
				entry.Expires = time.Time{}
				s.pending[ref] = entry
				_ = s.persistLocked()
				slog.Warn("credential material retirement deferred", "event", "credential_material_retire_deferred", "provider", entry.Provider, "ref", entry.Ref, "state", stateRetired, "error", err)
				continue
			}
		}
		delete(s.pending, ref)
	}
	return s.persistLocked()
}

func (s *Service) persistLocked() error {
	if s.path == "" {
		return nil
	}
	entries := make([]Record, 0, len(s.pending))
	for _, entry := range s.pending {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Ref < entries[j].Ref })
	raw, err := json.MarshalIndent(registry{SchemaVersion: 1, Records: entries}, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return atomicfile.Replace(s.path, raw, 0o600)
}

func validateEntry(entry Record) error {
	if strings.TrimSpace(entry.Provider) == "" || strings.TrimSpace(entry.Ref) == "" {
		return fmt.Errorf("credential material registry record requires provider and ref")
	}
	if entry.State != statePending && entry.State != stateCommitted && entry.State != stateRetired {
		return fmt.Errorf("credential material registry record %q has unknown state %q", entry.Ref, entry.State)
	}
	if !strings.HasPrefix(entry.Ref, "secret:cred_") && !strings.HasPrefix(entry.Ref, "secretfile:cred_") {
		return fmt.Errorf("credential material registry record %q has invalid owned reference", entry.Ref)
	}
	if entry.State == statePending && entry.Expires.IsZero() {
		return fmt.Errorf("pending credential material registry record %q has no expiry", entry.Ref)
	}
	if entry.State != statePending && !entry.Expires.IsZero() {
		return fmt.Errorf("credential material registry record %q has an expiry in state %q", entry.Ref, entry.State)
	}
	return nil
}

func newMaterialID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("allocate credential material identity: %w", err)
	}
	return fmt.Sprintf("cred_%x", raw[:]), nil
}
