package configstore

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"

	"gopkg.in/yaml.v3"

	"github.com/swobuforge/swobu/internal/routing"
)

var (
	ErrLocked      = errors.New("routing config is locked by another process")
	ErrStoreClosed = errors.New("routing config store is closed")
	ErrReadOnly    = errors.New("config_read_only: externally managed config cannot be changed")
)

type storeState uint8

const (
	storeOpen storeState = iota
	storeClosed
)

// Store serializes semantic edits and publishes one immutable routing snapshot
// at the rename commit point. Directory sync is a best-effort durability step
// after publication and never changes the result of a committed update.
type Store struct {
	openedDigest       [32]byte
	path               string
	readOnly           bool
	lock               *os.File
	mu                 sync.Mutex
	referenceValidator ReferencePublisher
	// nodePublisher atomically commits routing while preserving the latest
	// inline Connection section in the one managed Node document.
	nodePublisher        func([]byte) error
	hasNodeOwnedSections bool
	state                storeState
	snapshot             atomic.Pointer[routing.Config]
	ops                  fileOps
	logger               *slog.Logger
	schemaVersion        int
	deletions            map[string]deletionReceipt
	creations            map[string]creationReceipt
	creationAudits       map[string]WorkspaceCreationAudit
	deletionAudits       map[string]WorkspaceDeletionAudit
	displayNameAudits    []WorkspaceDisplayNameAudit
}

// Open opens an existing routing configuration and holds its path lock until
// Store.Close.
func Open(path string) (*Store, error) {
	return open(path, false, nil)
}

// OpenReadOnly reads an existing externally managed routing document without
// creating a directory, lock, temp file, or modifying its permissions. The
// caller is responsible for restarting Swobu after changing the file; there is
// deliberately no watcher or second writable authority.
func OpenReadOnly(path string) (*Store, error) {
	if filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml" {
		return nil, fmt.Errorf("routing config must be YAML")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read externally managed routing config: %w", err)
	}
	store := &Store{path: path, readOnly: true, ops: defaultFileOps(), logger: slog.Default(), openedDigest: sha256.Sum256(raw)}
	config, err := store.decodeDocument(raw)
	if err != nil {
		return nil, err
	}
	config, err = stampWorkspaceRevisions(config, config)
	if err != nil {
		return nil, err
	}
	store.snapshot.Store(&config)
	return store, nil
}

// OpenedNodeRevision identifies the exact bytes used to initialize the
// Workspace snapshot. Bootstrap compares it with the subsequently opened
// common Node file so two reads cannot silently compose different generations
// of a concurrently replaced externally managed configuration.
func (s *Store) OpenedNodeRevision() string {
	if s == nil {
		return ""
	}
	return hex.EncodeToString(s.openedDigest[:])
}

// ReadOnly reports whether this store is controlled by external configuration.
func (s *Store) ReadOnly() bool { return s != nil && s.readOnly }

// OpenOrCreate opens a routing configuration, creating the canonical empty
// document under the lifetime path lock when the file does not yet exist.
func OpenOrCreate(path string) (*Store, error) {
	return open(path, true, nil)
}

// OpenOrCreateAfterRecovery holds the routing lifetime lock while recovery runs,
// before reading or creating routing state. The callback must not reenter this
// store. Its failure releases the lock without opening normal services.
func OpenOrCreateAfterRecovery(path string, recover func() error) (*Store, error) {
	if recover == nil {
		return nil, errors.New("startup recovery is required")
	}
	return open(path, true, recover)
}

func open(path string, create bool, recover func() error) (*Store, error) {
	if filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml" {
		return nil, fmt.Errorf("routing config must be YAML")
	}
	if err := ensureConfigDirectory(filepath.Dir(path)); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open config lock: %w", err)
	}
	lockNamed, nameErr := os.Lstat(lock.Name())
	lockOpened, openedErr := lock.Stat()
	if nameErr != nil || openedErr != nil || !lockNamed.Mode().IsRegular() || !os.SameFile(lockNamed, lockOpened) {
		_ = lock.Close()
		return nil, fmt.Errorf("validate config lock identity: lock was replaced or is not regular")
	}
	if err := acquireFileLock(lock); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("%w: %s", ErrLocked, path)
	}
	if recover != nil {
		if err := recover(); err != nil {
			_ = releaseFileLock(lock)
			_ = lock.Close()
			return nil, err
		}
	}
	ops := defaultFileOps()
	raw, err := readConfig(path)
	if create && errors.Is(err, os.ErrNotExist) {
		raw, err = encode(routing.Config{})
		if err != nil {
			_ = releaseFileLock(lock)
			_ = lock.Close()
			return nil, fmt.Errorf("encode initial routing config: %w", err)
		}
		if err := replaceFile(path, raw, ops); err != nil {
			_ = releaseFileLock(lock)
			_ = lock.Close()
			return nil, fmt.Errorf("create routing config: %w", err)
		}
		if err := ops.syncDir(filepath.Dir(path)); err != nil {
			slog.Default().Warn("routing config directory sync failed after initial commit", "path", path, "error", err)
		}
		raw, err = readConfig(path)
	}
	if err != nil {
		_ = releaseFileLock(lock)
		_ = lock.Close()
		return nil, fmt.Errorf("read routing config: %w", err)
	}
	store := &Store{path: path, lock: lock, ops: ops, logger: slog.Default(), openedDigest: sha256.Sum256(raw)}
	config, err := store.decodeDocument(raw)
	if err != nil {
		_ = releaseFileLock(lock)
		_ = lock.Close()
		return nil, err
	}
	// Reopen rotates ephemeral publication authority without resetting the
	// validated durable management revisions decoded from the document.
	config, err = stampWorkspaceRevisions(config, config)
	if err != nil {
		_ = releaseFileLock(lock)
		_ = lock.Close()
		return nil, err
	}
	store.snapshot.Store(&config)
	store.hasNodeOwnedSections = hasNodeOwnedSections(raw)
	store.cleanupStaleTemps()
	return store, nil
}

// readConfig reads a regular local config file from the same handle inspected
// for identity. Managed configuration is serialized through the lifetime lock.
// Swobu does not impose an OS-specific ACL policy on a trusted local operator.
func readConfig(path string) ([]byte, error) {
	named, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !named.Mode().IsRegular() {
		return nil, errors.New("routing config is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(named, opened) {
		return nil, errors.New("routing config changed during inspection")
	}
	return io.ReadAll(file)
}

func (s *Store) Config() routing.Config {
	if s == nil || s.snapshot.Load() == nil {
		return routing.Config{}
	}
	return s.snapshot.Load().Clone()
}

func (s *Store) GetWorkspace(_ context.Context, slug routing.WorkspaceSlug) (routing.Workspace, error) {
	workspace, ok := s.Config().Workspace(slug)
	if !ok {
		return routing.Workspace{}, fmt.Errorf("%w: workspace %q", routing.ErrNotFound, slug.String())
	}
	return workspace, nil
}

func (s *Store) Update(ctx context.Context, edit func(routing.Config) (routing.Config, error)) (routing.Config, error) {
	return s.UpdatePrepared(ctx, edit, nil)
}

func (s *Store) UpdatePrepared(ctx context.Context, edit func(routing.Config) (routing.Config, error), beforeCommit func(current, next routing.Config) error) (routing.Config, error) {
	if s == nil {
		return routing.Config{}, ErrStoreClosed
	}
	if err := ctx.Err(); err != nil {
		return routing.Config{}, err
	}
	if edit == nil {
		return routing.Config{}, fmt.Errorf("edit is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updateLocked(ctx, edit, beforeCommit, s.deletions, s.creations)
}

// updateLocked is the sole routing publication transaction. Receipt candidates
// become live only at the same rename commit point as their routing change.
func (s *Store) updateLocked(ctx context.Context, edit func(routing.Config) (routing.Config, error), beforeCommit func(routing.Config, routing.Config) error, receipts map[string]deletionReceipt, creations map[string]creationReceipt) (routing.Config, error) {
	switch s.state {
	case storeClosed:
		return routing.Config{}, ErrStoreClosed
	}
	if s.readOnly {
		return routing.Config{}, ErrReadOnly
	}
	if err := ctx.Err(); err != nil {
		return routing.Config{}, err
	}
	current := s.snapshot.Load().Clone()
	next, err := edit(current)
	if err != nil {
		return routing.Config{}, err
	}
	next, err = routing.NewConfig(next.Workspaces())
	if err != nil {
		return routing.Config{}, err
	}
	next, err = stampWorkspaceRevisions(current, next)
	if err != nil {
		return routing.Config{}, err
	}
	return s.publishLocked(current, next, beforeCommit, receipts, creations, s.displayNameAudits)
}

// publishLocked commits an already validated and stamped candidate and its
// retry state. Callers hold mu; no second stamp may change receipt-bound identity.
func (s *Store) publishLocked(current, next routing.Config, beforeCommit func(routing.Config, routing.Config) error, receipts map[string]deletionReceipt, creations map[string]creationReceipt, displayAudits []WorkspaceDisplayNameAudit, reviewedConnection ...*ReviewedConnection) (routing.Config, error) {
	if s.readOnly {
		return routing.Config{}, ErrReadOnly
	}
	var reviewed *ReviewedConnection
	if len(reviewedConnection) > 0 {
		reviewed = reviewedConnection[0]
	}
	audits := make(map[string]WorkspaceCreationAudit, len(s.creationAudits)+1)
	for identity, audit := range s.creationAudits {
		audits[identity] = audit
	}
	for _, receipt := range creations {
		if receipt.Audit != nil {
			audits[receipt.Audit.Incarnation] = *receipt.Audit
		}
	}
	deletionAudits := make(map[string]WorkspaceDeletionAudit, len(s.deletionAudits)+1)
	for identity, audit := range s.deletionAudits {
		deletionAudits[identity] = audit
	}
	for _, receipt := range receipts {
		if receipt.Audit != nil {
			deletionAudits[receipt.Audit.ETag] = *receipt.Audit
		}
	}
	raw, err := encodeDocumentWithReceipts(next, s.schemaVersion, receipts, creations, audits, deletionAudits, displayAudits)
	if err != nil {
		return routing.Config{}, err
	}
	if beforeCommit != nil {
		if err := beforeCommit(current, next); err != nil {
			return routing.Config{}, err
		}
	}
	// The Connection reference guard owns the registry read lock through the
	// durable routing write. A validation-only callback would allow an edit
	// between reference inspection and publication.
	commit := func() error {
		if err := s.publishRoutingBytes(raw); err != nil {
			return err
		}
		s.snapshot.Store(&next)
		s.deletions = receipts
		s.creations = creations
		s.creationAudits = audits
		s.deletionAudits = deletionAudits
		s.displayNameAudits = displayAudits
		return nil
	}
	if s.referenceValidator != nil {
		if err := s.referenceValidator(next.Clone(), reviewed, commit); err != nil {
			return routing.Config{}, err
		}
	} else {
		if reviewed != nil {
			return routing.Config{}, errors.New("reviewed Connection publication requires reference admission")
		}
		if err := commit(); err != nil {
			return routing.Config{}, err
		}
	}
	if err := s.ops.syncDir(filepath.Dir(s.path)); err != nil {
		s.logger.Warn("routing config directory sync failed after commit", "path", s.path, "error", err)
	}
	return next.Clone(), nil
}

// Revisions are process-local snapshot authority. Fresh random identities on
// reopen prevent cross-store reuse; pending Exchanges do not survive restart.
// Compare settings without a caller-supplied stamp so restoring an old Config
// cannot restore its authority. Only successful publication exposes new stamps.
func stampWorkspaceRevisions(current, next routing.Config) (routing.Config, error) {
	workspaces := next.Workspaces()
	for i, workspace := range workspaces {
		previous, exists := current.Workspace(workspace.Slug())
		// Only the same immutable slug can retain a lifetime. Caller-carried
		// stamps never transfer authority from a removed address to a new one.
		revision := previous.ConfigurationRevision()
		management := previous.ManagementRevision()
		incarnation := previous.ManagementIncarnation()
		changed := !exists || !reflect.DeepEqual(previous.WithConfigurationRevision("").WithManagementRevision(0).WithManagementIncarnation(""), workspace.WithConfigurationRevision("").WithManagementRevision(0).WithManagementIncarnation(""))
		if !exists {
			management = 1
			var identity [32]byte
			if _, err := rand.Read(identity[:]); err != nil {
				return routing.Config{}, fmt.Errorf("issue Workspace incarnation: %w", err)
			}
			incarnation = hex.EncodeToString(identity[:])
		} else if changed {
			if management == ^uint64(0) {
				return routing.Config{}, fmt.Errorf("Workspace management revision exhausted")
			}
			management++
		}
		if changed || revision == "" {
			var identity [32]byte
			if _, err := rand.Read(identity[:]); err != nil {
				return routing.Config{}, fmt.Errorf("issue Workspace configuration revision: %w", err)
			}
			revision = hex.EncodeToString(identity[:])
		}
		workspaces[i] = workspace.WithConfigurationRevision(revision).WithManagementRevision(management).WithManagementIncarnation(incarnation)
	}
	return routing.NewConfig(workspaces)
}

// Close is terminal and one-shot: it marks the store closed, then releases the
// cross-process file lock and closes the lock file, joining both errors. A close
// operation is not retryable, so it never leaves the store in an "open but
// unlocked" state.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == storeClosed {
		return nil
	}
	s.state = storeClosed
	if s.lock == nil {
		return nil
	}
	lock := s.lock
	s.lock = nil
	return errors.Join(releaseFileLock(lock), lock.Close())
}

func ensureConfigDirectory(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create config directory: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect config directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("config directory %q is not a directory", path)
	}
	return nil
}

func (s *Store) cleanupStaleTemps() {
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(s.path), "."+filepath.Base(s.path)+".tmp-*"))
	for _, match := range matches {
		_ = os.Remove(match)
	}
}

// ValidateDocument uses the complete reopen decoder, including retained command
// receipts and audit facts, without file I/O or publication.
func ValidateDocument(raw []byte) (routing.Config, error) {
	return (&Store{}).decodeDocument(raw)
}

func (store *Store) decodeDocument(raw []byte) (routing.Config, error) {
	config, err := decode(raw)
	if err != nil {
		return routing.Config{}, err
	}
	store.deletions, err = decodeDeletionReceipts(raw)
	if err != nil {
		return routing.Config{}, err
	}
	store.schemaVersion, err = documentSchema(raw)
	if err == nil {
		store.creations, err = decodeCreationReceipts(raw)
	}
	if err == nil {
		store.creationAudits, err = decodeWorkspaceCreationAudits(raw)
	}
	if err == nil {
		store.deletionAudits, err = decodeWorkspaceDeletionAudits(raw)
	}
	if err == nil {
		store.displayNameAudits, err = decodeDisplayNameAudits(raw)
	}
	if err != nil {
		return routing.Config{}, err
	}
	return config, nil
}

// InstallNodePublisher binds the daemon-owned one-file publication adapter
// before commands are served. It never changes a standalone store's writer.
func (s *Store) InstallNodePublisher(publish func([]byte) error) error {
	if s == nil {
		return ErrStoreClosed
	}
	if publish == nil {
		return errors.New("Node publisher is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == storeClosed {
		return ErrStoreClosed
	}
	if s.readOnly {
		return ErrReadOnly
	}
	if s.nodePublisher != nil {
		return errors.New("Node publisher already installed")
	}
	s.nodePublisher = publish
	return nil
}

func (s *Store) publishRoutingBytes(raw []byte) error {
	if s.nodePublisher != nil {
		return s.nodePublisher(raw)
	}
	// A standalone routing writer must never erase a Connection document it
	// does not own. Daemon startup binds the publisher before any mutations.
	if s.hasNodeOwnedSections {
		return errors.New("inline Node authorities require Node publication")
	}
	return replaceFile(s.path, raw, s.ops)
}

// hasNodeOwnedSections checks top-level decoded keys, not comments or nested
// objects. A standalone routing writer may not discard any independent owner.
func hasNodeOwnedSections(raw []byte) bool {
	var fields map[string]any
	if err := yaml.Unmarshal(raw, &fields); err != nil {
		return false
	}
	for _, section := range []string{"connections", "principals", "publications", "accounting"} {
		if _, present := fields[section]; present {
			return true
		}
	}
	return false
}
