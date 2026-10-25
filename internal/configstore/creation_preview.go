package configstore

import (
	"context"
	"errors"

	"github.com/swobuforge/swobu/internal/routing"
)

// PreviewWorkspaceCreation checks the same draft/schema/availability boundary
// as fresh apply without issuing lifetime authority, writing bytes or retaining
// retry state. It reserves nothing. Admission runs under mu and cannot reenter.
func (s *Store) PreviewWorkspaceCreation(ctx context.Context, draft routing.Workspace, authorize func(context.Context) error) error {
	if s == nil {
		return ErrStoreClosed
	}
	if authorize == nil {
		return errors.New("creation preview requires trusted admission")
	}
	if _, _, err := CanonicalWorkspaceCreationDraft(draft); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == storeClosed {
		return ErrStoreClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := authorize(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.checkCreationAvailability(draft)
}

// CanonicalWorkspaceCreationDraft strips caller-carried authority before compiling the
// validated semantic input used by preview and retry intent hashing.
func CanonicalWorkspaceCreationDraft(draft routing.Workspace) (routing.Workspace, []byte, error) {
	draft = draft.WithConfigurationRevision("").WithManagementRevision(0).WithManagementIncarnation("")
	config, err := routing.NewConfig([]routing.Workspace{draft})
	if err != nil {
		return routing.Workspace{}, nil, err
	}
	canonical, err := encodeDocument(config, routing.ReferenceSchemaVersion)
	return draft, canonical, err
}

// checkCreationAvailability requires mu. Replay deliberately bypasses this
// fresh-creation check so it cannot substitute a replacement lifetime.
func (s *Store) checkCreationAvailability(draft routing.Workspace) error {
	if _, exists := s.snapshot.Load().Workspace(draft.Slug()); exists {
		return routing.ErrConflict
	}
	return nil
}
