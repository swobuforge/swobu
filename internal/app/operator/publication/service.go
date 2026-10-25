package publication

import (
	"context"
	"errors"

	"github.com/swobuforge/swobu/internal/publications"
	"github.com/swobuforge/swobu/internal/routing"
)

var ErrUnavailable = errors.New("relay unavailable")

type Service struct {
	Store          *publications.Store
	Exists         func(routing.WorkspaceSlug) bool
	Ready          func(context.Context) (func(), error)
	StopIfInactive func()
	RelayURL       func() string
}

type Snapshot struct {
	Workspace string `json:"workspace"`
	Published bool   `json:"published"`
	Address   string `json:"address"`
	URL       string `json:"url,omitempty"`
	Revision  string `json:"revision"`
}

func (s Service) Read(ctx context.Context, slug routing.WorkspaceSlug, authorize func(context.Context) error) (Snapshot, error) {
	if err := s.check(ctx, slug, authorize); err != nil {
		return Snapshot{}, err
	}
	return s.snapshot(slug)
}

func (s Service) check(ctx context.Context, slug routing.WorkspaceSlug, authorize func(context.Context) error) error {
	if s.Store == nil || s.Exists == nil || !s.Exists(slug) {
		return errors.New("Workspace publication unavailable")
	}
	if authorize == nil {
		return errors.New("Node admission required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return authorize(ctx)
}

func (s Service) Set(ctx context.Context, slug routing.WorkspaceSlug, published bool, expected string, authorize func(context.Context) error) (Snapshot, error) {
	if expected == "" {
		return Snapshot{}, errors.New("publication revision required")
	}
	if err := s.check(ctx, slug, authorize); err != nil {
		return Snapshot{}, err
	}
	unlock := s.Store.LockLifecycle()
	defer unlock()
	if s.Exists == nil || !s.Exists(slug) {
		return Snapshot{}, errors.New("Workspace publication unavailable")
	}
	if published {
		if s.Ready == nil {
			return Snapshot{}, ErrUnavailable
		}
		release, err := s.Ready(ctx)
		if err != nil {
			return Snapshot{}, ErrUnavailable
		}
		if release != nil {
			defer release()
		}
	}
	if err := s.Store.Set(slug, published, expected, func() error { return authorize(ctx) }); err != nil {
		return Snapshot{}, err
	}
	if !published && s.StopIfInactive != nil {
		defer s.StopIfInactive()
	}
	// A caller may unpublish its own Workspace. Return that committed outcome
	// once; rechecking its withdrawn reachability here would misreport success.
	return s.snapshot(slug)
}

func (s Service) snapshot(slug routing.WorkspaceSlug) (Snapshot, error) {
	result := Snapshot{Workspace: slug.String(), Address: "relay"}
	if s.RelayURL != nil {
		result.URL = s.RelayURL()
	}
	err := s.Store.WithReviewSnapshot(func(revision string, workspaces []string) error {
		result.Revision = revision
		for _, candidate := range workspaces {
			if candidate == slug.String() {
				result.Published = true
				break
			}
		}
		return nil
	})
	return result, err
}
