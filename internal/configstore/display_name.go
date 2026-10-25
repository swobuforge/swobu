package configstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

// SetWorkspaceDisplayNameChecked compares exact lifetime/revision and repeats
// trusted admission under the sole publication lock. It returns the committed
// snapshot, never a later read. Admission must not reenter this Store.
// Matching no-op still checks consent/admission but publishes no bytes.
func (s *Store) SetWorkspaceDisplayNameChecked(ctx context.Context, slug routing.WorkspaceSlug, name, expected string, authorize func(context.Context) error) (routing.Workspace, error) {
	if s == nil {
		return routing.Workspace{}, ErrStoreClosed
	}
	if expected == "" || authorize == nil {
		return routing.Workspace{}, errors.New("display name update requires exact Workspace revision and trusted admission")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == storeClosed {
		return routing.Workspace{}, ErrStoreClosed
	}

	if s.readOnly {
		return routing.Workspace{}, ErrReadOnly
	}
	if err := ctx.Err(); err != nil {
		return routing.Workspace{}, err
	}
	if err := authorize(ctx); err != nil {
		return routing.Workspace{}, err
	}
	if err := ctx.Err(); err != nil {
		return routing.Workspace{}, err
	}
	workspace, exists := s.snapshot.Load().Workspace(slug)
	if !exists {
		return routing.Workspace{}, routing.ErrNotFound
	}
	if fmt.Sprintf("%s-%d", workspace.ManagementIncarnation(), workspace.ManagementRevision()) != expected {
		return routing.Workspace{}, ErrStaleRevision
	}
	updated, err := workspace.WithDisplayName(name)
	if err != nil {
		return routing.Workspace{}, err
	}
	if updated.DisplayName() == workspace.DisplayName() {
		return workspace, nil
	}
	current := s.snapshot.Load().Clone()
	next, err := current.SetWorkspaceDisplayName(slug, name)
	if err != nil {
		return routing.Workspace{}, err
	}
	next, err = stampWorkspaceRevisions(current, next)
	if err != nil {
		return routing.Workspace{}, err
	}
	audits := append([]WorkspaceDisplayNameAudit(nil), s.displayNameAudits...)
	audits = append(audits, WorkspaceDisplayNameAudit{Action: "workspace.display_name", Caller: accessidentity.Attribution(ctx).String(), Workspace: slug.String(), ETag: expected, Before: workspace.DisplayName(), After: updated.DisplayName(), CommittedAt: time.Now().UTC()})
	if err := ctx.Err(); err != nil {
		return routing.Workspace{}, err
	}
	next, err = s.publishLocked(current, next, nil, s.deletions, s.creations, audits)
	if err != nil {
		return routing.Workspace{}, err
	}
	committed, _ := next.Workspace(slug)
	return committed, nil
}
