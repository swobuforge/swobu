package limits

import (
	"context"
	"errors"
	"time"

	"github.com/swobuforge/swobu/internal/accounting"
	"github.com/swobuforge/swobu/internal/app/operator/adminapproval"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
)

// Service shares the existing Control review store; it never persists sessions.
type Service struct {
	store   *accounting.Store
	reviews *adminapproval.Service
	exists  func(string) bool
}

func New(store *accounting.Store, reviews *adminapproval.Service, exists func(string) bool) Service {
	return Service{store: store, reviews: reviews, exists: exists}
}

func (s Service) check(ctx context.Context, workspace string, authorize func(context.Context) error) error {
	if s.store == nil || s.exists == nil || !s.exists(workspace) {
		return errors.New("Workspace policy unavailable")
	}
	if authorize == nil {
		return errors.New("Node admission required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return authorize(ctx)
}

type Snapshot struct {
	Status   accounting.Status `json:"status"`
	Revision string            `json:"revision"`
}

func (s Service) Read(ctx context.Context, workspace string, authorize func(context.Context) error) (Snapshot, error) {
	if err := s.check(ctx, workspace, authorize); err != nil {
		return Snapshot{}, err
	}
	status, revision := s.store.StatusWithRevision(workspace)
	return Snapshot{status, revision}, nil
}

func (s Service) Preview(ctx context.Context, workspace string, limit accounting.Limit, authorize func(context.Context) error) (adminapproval.Approval, error) {
	if err := s.check(ctx, workspace, authorize); err != nil {
		return adminapproval.Approval{}, err
	}
	if s.reviews == nil {
		return adminapproval.Approval{}, errors.New("policy review unavailable")
	}
	if err := limit.Validate(); err != nil {
		return adminapproval.Approval{}, err
	}
	return s.reviews.Create(workspace, "limits.set", s.store.Revision(), reviewedLimit{limit, accessidentity.Attribution(ctx).String()}, 2*time.Minute)
}

func (s Service) Apply(ctx context.Context, workspace, reviewID string, limit accounting.Limit, authorize func(context.Context) error) error {
	if err := s.check(ctx, workspace, authorize); err != nil {
		return err
	}
	if s.reviews == nil {
		return errors.New("policy review unavailable")
	}
	if err := limit.Validate(); err != nil {
		return err
	}
	review, err := s.reviews.Get(reviewID)
	if err != nil || review.Workspace != workspace || review.Operation != "limits.set" {
		return errors.New("invalid policy review")
	}
	if err := s.reviews.Consume(review.ID, workspace, "limits.set", review.Revision, reviewedLimit{limit, accessidentity.Attribution(ctx).String()}); err != nil {
		return err
	}
	return s.store.ApplyReviewedLimitChecked(ctx, workspace, limit, accounting.LimitChange{ApprovalID: review.ID, Workspace: workspace, Actor: accessidentity.Attribution(ctx).String(), Digest: review.Digest, Revision: review.Revision}, authorize)
}

func (s Service) Clear(ctx context.Context, workspace, revision string, authorize func(context.Context) error) error {
	if err := s.check(ctx, workspace, authorize); err != nil {
		return err
	}
	return s.store.ClearLimitChecked(ctx, workspace, revision, authorize)
}

// The existing review digest binds the admitted actor as well as the policy;
// another delegated administrator cannot consume a copied review identifier.
type reviewedLimit struct {
	Limit  accounting.Limit
	Caller string
}
