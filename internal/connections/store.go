package connections

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

var ErrNotFound = errors.New("Connection not found")
var ErrAlreadyExists = errors.New("Connection already exists")

// Store owns the inline Node Connection publication lock. The Node document
// is the only durable configuration authority. Readers get immutable
// values, ordered by stable ID. Persistence completes before memory publication.
// Workspace reference coordination belongs to the application transaction, not
// a second reverse index in this file.
type Store struct {
	staged         map[string]StagedCredential
	retired        map[string]string
	mu             sync.RWMutex
	values         map[connectionidentity.ID]Connection
	checks         map[connectionidentity.ID]Check
	audits         []Audit
	replacements   map[string]replacementReceipt
	creations      map[string]creationReceipt
	publishSection func([]byte) error
}
type storeDocument struct {
	Staged        map[string]StagedCredential   `json:"staged_credentials,omitempty"`
	Retired       map[string]string             `json:"retired_credentials,omitempty"`
	Replacements  map[string]replacementReceipt `json:"replacements,omitempty"`
	SchemaVersion int                           `json:"schema_version"`
	Audits        []Audit                       `json:"audits,omitempty"`
	Connections   []record                      `json:"connections"`
	Creations     map[string]creationReceipt    `json:"creations,omitempty"`
}

// OpenInline loads Connections from the managed Node YAML document and
// delegates durable publication to that same file. There is no Connection
// sidecar, second lockfile, or fallback to historical source registries.
// The writer callback runs under the registry lock and must not reenter it.
func OpenInline(raw []byte, publish func([]byte) error, supports routing.ProviderSupport) (*Store, error) {
	if publish == nil {
		return nil, errors.New("inline Connection publisher is required")
	}
	store := &Store{values: map[connectionidentity.ID]Connection{}, publishSection: publish}
	if len(raw) == 0 {
		return store, nil
	}
	if err := store.decodeDocument(raw, supports); err != nil {
		return nil, fmt.Errorf("decode inline Connections: %w", err)
	}
	return store, nil
}

// ValidateDocument uses the normal registry reopen decoder without storage
// effects, including retry receipts, retained audit and credential retirement.
func ValidateDocument(raw []byte, supports routing.ProviderSupport) ([]Connection, error) {
	store := &Store{values: map[connectionidentity.ID]Connection{}}
	if err := store.decodeDocument(raw, supports); err != nil {
		return nil, err
	}
	return store.List(), nil
}

func (store *Store) decodeDocument(raw []byte, supports routing.ProviderSupport) error {
	var document storeDocument
	if err := strictDecode(raw, &document); err != nil {
		return err
	}
	if document.SchemaVersion != 1 || document.Connections == nil {
		return errors.New("unsupported Connection registry document")
	}
	for _, value := range document.Connections {
		connection, err := decodeRecord(value, supports)
		if err != nil {
			return err
		}
		if _, exists := store.values[connection.ID()]; exists {
			return errors.New("duplicate Connection identity")
		}
		store.values[connection.ID()] = connection
	}
	if err := validateCreationReceipts(document.Creations, supports); err != nil {
		return err
	}
	for _, audit := range document.Audits {
		if err := audit.Validate(); err != nil {
			return err
		}
	}
	if err := validateReplacementReceipts(document.Replacements, supports); err != nil {
		return err
	}
	store.replacements = document.Replacements
	if err := validateRetirements(store.values, document.Retired); err != nil {
		return err
	}
	if err := validateStaging(document.Staged); err != nil {
		return err
	}
	store.staged = document.Staged
	store.retired = document.Retired
	store.audits = document.Audits
	store.creations = document.Creations
	return nil
}

func (s *Store) Get(id connectionidentity.ID) (Connection, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.values[id]
	if !ok {
		return Connection{}, ErrNotFound
	}
	return value, nil
}
func (s *Store) List() []Connection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return orderedConnections(s.values)
}
func orderedConnections(values map[connectionidentity.ID]Connection) []Connection {
	result := make([]Connection, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID().String() < result[j].ID().String() })
	return result
}

// Publish inserts revision 1 or replaces exactly one inspected lifetime. A
// caller cannot reset creation time or jump revisions. Reference admission and
// audit must be coordinated by the owning application before exposing commands.
func (s *Store) Publish(candidate Connection, expected Revision) error {
	return s.publishChecked(context.Background(), candidate, expected, "", nil)
}

// PublishChecked rechecks trusted admission and commits an edit audit at the
// durable publication boundary. Scope comes from the admitted transport, never
// request JSON. Admission must not reenter this store.
func (s *Store) PublishChecked(ctx context.Context, candidate Connection, expected Revision, scope string, authorize func(context.Context) error) error {
	if authorize == nil || scope == "" {
		return errors.New("Connection publication requires admission")
	}
	return s.publishChecked(ctx, candidate, expected, scope, authorize)
}

func (s *Store) publishChecked(ctx context.Context, candidate Connection, expected Revision, scope string, authorize func(context.Context) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if authorize != nil {
		if err := authorize(ctx); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := Restore(candidate.id, candidate.label, candidate.spec, candidate.revision, candidate.createdAt, candidate.updatedAt); err != nil {
		return err
	}
	current, exists := s.values[candidate.ID()]
	if !exists {
		if expected.value != 0 {
			return ErrNotFound
		}
		if candidate.revision.value != 1 {
			return ErrRevisionConflict
		}
	} else {
		if expected.value == 0 {
			return ErrAlreadyExists
		}
		if expected != current.revision {
			return ErrRevisionConflict
		}
		// Reconstruct through the same validated transition; store callers cannot
		// bypass kind/provider identity checks by constructing a restored snapshot.
		checked, err := current.EditSameIdentity(expected, candidate.label, candidate.spec, candidate.updatedAt)
		if err != nil {
			return err
		}
		if candidate.revision != checked.revision || !candidate.createdAt.Equal(current.createdAt) {
			return ErrRevisionConflict
		}
	}
	values := make(map[connectionidentity.ID]Connection, len(s.values)+1)
	for id, value := range s.values {
		values[id] = value
	}
	values[candidate.ID()] = candidate
	if err := ctx.Err(); err != nil {
		return err
	}
	if scope != "" {
		if !exists {
			return errors.New("checked Connection edits require an existing identity")
		}
		audit := Audit{Action: "connection.edit", Caller: scope, ConnectionID: candidate.ID().String(), BeforeRevision: current.Revision().String(), AfterRevision: candidate.Revision().String(), CommittedAt: time.Now().UTC()}
		return s.publishDocument(values, s.creations, appendAudit(s.audits, audit))
	}
	return s.publish(values)
}
func (s *Store) publish(values map[connectionidentity.ID]Connection) error {
	return s.publishDocument(values, s.creations, s.audits)
}

func (s *Store) publishDocument(values map[connectionidentity.ID]Connection, receipts map[string]creationReceipt, audits []Audit) error {
	return s.publishState(values, receipts, s.replacements, audits)
}

func (s *Store) publishState(values map[connectionidentity.ID]Connection, receipts map[string]creationReceipt, replacements map[string]replacementReceipt, audits []Audit) error {
	return s.writeState(values, receipts, replacements, audits, s.retired)
}

func (s *Store) publishRetirements(retired map[string]string) error {
	return s.writeState(s.values, s.creations, s.replacements, s.audits, retired)
}

func (s *Store) writeState(values map[connectionidentity.ID]Connection, receipts map[string]creationReceipt, replacements map[string]replacementReceipt, audits []Audit, retired map[string]string) error {
	return s.writeDocument(values, receipts, replacements, audits, retired, s.staged)
}

func (s *Store) writeStaging(staged map[string]StagedCredential) error {
	return s.writeDocument(s.values, s.creations, s.replacements, s.audits, s.retired, staged)
}

func (s *Store) writeDocument(values map[connectionidentity.ID]Connection, receipts map[string]creationReceipt, replacements map[string]replacementReceipt, audits []Audit, retired map[string]string, staged map[string]StagedCredential) error {
	staged = trackRemovedOwnedBindings(s.values, values, staged, time.Now().UTC())
	if err := validateStaging(staged); err != nil {
		return err
	}
	if err := validateRetirements(values, retired); err != nil {
		return err
	}
	document := storeDocument{Staged: staged, Retired: retired, SchemaVersion: 1, Connections: []record{}, Creations: receipts, Replacements: replacements, Audits: audits}
	for _, value := range orderedConnections(values) {
		encoded, err := encodeRecord(value)
		if err != nil {
			return err
		}
		document.Connections = append(document.Connections, encoded)
	}
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return errors.New("cannot encode Connection registry")
	}
	raw = append(raw, '\n')
	if err := s.publishSection(raw); err != nil {
		return errors.New("cannot publish Connection registry")
	}
	s.values = values
	s.creations = receipts
	s.audits = audits
	s.replacements = replacements
	s.retired = retired
	s.staged = staged
	return nil
}
