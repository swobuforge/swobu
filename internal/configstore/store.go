package configstore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/swobuforge/swobu/internal/routing"
)

var (
	ErrLocked      = errors.New("routing config is locked by another process")
	ErrStoreClosed = errors.New("routing config store is closed")
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
	path     string
	lock     *os.File
	mu       sync.Mutex
	state    storeState
	snapshot atomic.Pointer[routing.Config]
	ops      fileOps
	logger   *slog.Logger
}

// Open opens an existing routing configuration and holds its path lock until
// Store.Close.
func Open(path string) (*Store, error) {
	return open(path, false)
}

// OpenOrCreate opens a routing configuration, creating the canonical empty
// document under the lifetime path lock when the file does not yet exist.
func OpenOrCreate(path string) (*Store, error) {
	return open(path, true)
}

func open(path string, create bool) (*Store, error) {
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
	if err := lock.Chmod(0o600); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("secure config lock: %w", err)
	}
	if err := acquireFileLock(lock); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("%w: %s", ErrLocked, path)
	}
	ops := defaultFileOps()
	raw, err := os.ReadFile(path)
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
		err = nil
	}
	if err != nil {
		_ = releaseFileLock(lock)
		_ = lock.Close()
		return nil, fmt.Errorf("read routing config: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = releaseFileLock(lock)
		_ = lock.Close()
		return nil, fmt.Errorf("secure routing config: %w", err)
	}
	config, err := decode(raw)
	if err != nil {
		_ = releaseFileLock(lock)
		_ = lock.Close()
		return nil, err
	}
	store := &Store{path: path, lock: lock, ops: ops, logger: slog.Default()}
	store.snapshot.Store(&config)
	store.cleanupStaleTemps()
	return store, nil
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
	switch s.state {
	case storeClosed:
		return routing.Config{}, ErrStoreClosed
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
	raw, err := encode(next)
	if err != nil {
		return routing.Config{}, err
	}
	if beforeCommit != nil {
		if err := beforeCommit(current, next); err != nil {
			return routing.Config{}, err
		}
	}
	if err := replaceFile(s.path, raw, s.ops); err != nil {
		return routing.Config{}, err
	}
	s.snapshot.Store(&next)
	if err := s.ops.syncDir(filepath.Dir(s.path)); err != nil {
		s.logger.Warn("routing config directory sync failed after commit", "path", s.path, "error", err)
	}
	return next.Clone(), nil
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
		if err := os.Chmod(path, 0o700); err != nil {
			return fmt.Errorf("secure config directory: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect config directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("config directory %q is not a directory", path)
	}
	if err := validatePrivateDirectory(info); err != nil {
		return fmt.Errorf("validate config directory %q: %w", path, err)
	}
	return nil
}

func (s *Store) cleanupStaleTemps() {
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(s.path), "."+filepath.Base(s.path)+".tmp-*"))
	for _, match := range matches {
		_ = os.Remove(match)
	}
}
