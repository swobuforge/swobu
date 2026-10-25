package workspaces

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/routing"
)

var (
	ErrServiceReviewRequired = errors.New("Service activation requires authority-expansion confirmation")
	ErrServiceReviewChanged  = errors.New("Service activation review changed")
)

// Services returns all released Services with a reviewed activation impact for
// disabled Services. The snapshot includes *effective*, unexpired grants and
// relay publication: a nil Service scope can confer future capabilities.
func (s Service) Services(ctx context.Context, raw string) ([]ServiceState, error) {
	if s.store == nil {
		return nil, CommandError{Unavailable, "store unavailable"}
	}
	slug, err := routing.ParseWorkspaceSlug(raw)
	if err != nil {
		return nil, commandError(err)
	}
	var result []ServiceState
	err = s.withServiceAuthoritySnapshot(func(accessRevision string, principals []accessstate.Access, publicationRevision string, published []string) error {
		workspace, err := s.store.GetWorkspace(ctx, slug)
		if err != nil {
			return err
		}
		for _, kind := range []routing.ServiceKind{routing.ServiceModels, routing.ServiceMCP, routing.ServiceSearch} {
			configured := false
			switch kind {
			case routing.ServiceModels:
				configured = len(workspace.Routes()) > 0
			case routing.ServiceMCP:
				configured = len(workspace.MCPMounts()) > 0
			case routing.ServiceSearch:
				configured = workspace.SearchConnectionID() != ""
			}
			row := ServiceState{Kind: string(kind), Enabled: workspace.HasService(kind), Configured: configured}
			if !row.Enabled {
				preview := serviceActivationReview(workspace, kind, accessRevision, principals, publicationRevision, published)
				row.ActivationReview = &preview
			}
			result = append(result, row)
		}
		return nil
	})
	if err != nil {
		return nil, commandError(err)
	}
	return result, nil
}

// SetServiceChecked supports no-impact activation and all checked disables.
// An expansion of existing grant or relay reachability must use
// SetServiceReviewed with an exact preview digest. This is enforced in the
// application owner, not exclusively in the Web presentation.
func (s Service) SetServiceChecked(ctx context.Context, raw, rawKind string, enabled bool, expected string, authorize func(context.Context) error) (Workspace, error) {
	return s.SetServiceReviewed(ctx, raw, rawKind, enabled, expected, "", authorize)
}

// SetServiceReviewed holds publication lifecycle, grant authority and routing
// publication in one lock order, then re-derives the reviewed impact. A stale
// Principal, publication or Workspace change is refused before any Node write.
func (s Service) SetServiceReviewed(ctx context.Context, raw, rawKind string, enabled bool, expected, digest string, authorize func(context.Context) error) (Workspace, error) {
	if s.store == nil {
		return Workspace{}, CommandError{Unavailable, "store unavailable"}
	}
	slug, err := routing.ParseWorkspaceSlug(raw)
	if err != nil {
		return Workspace{}, commandError(err)
	}
	kind, err := routing.ParseServiceKind(rawKind)
	if err != nil {
		return Workspace{}, CommandError{InvalidArgument, err.Error()}
	}
	if expected == "" || authorize == nil {
		return Workspace{}, CommandError{InvalidArgument, "Service change requires a strong Workspace ETag and local authority"}
	}

	if !enabled {
		value, err := s.store.SetWorkspaceServiceChecked(ctx, slug, kind, false, expected, authorize)
		if errors.Is(err, configstore.ErrStaleRevision) {
			return Workspace{}, err
		}
		if err != nil {
			return Workspace{}, commandError(err)
		}
		return projectWorkspace(value), nil
	}
	if s.publications != nil {
		unlock := s.publications.LockLifecycle()
		defer unlock()
	}
	if s.bindings != nil {
		unlock := s.bindings.Lock()
		defer unlock()
	}
	var committed routing.Workspace
	err = s.withServiceAuthoritySnapshot(func(accessRevision string, principals []accessstate.Access, publicationRevision string, published []string) error {
		current, err := s.store.GetWorkspace(ctx, slug)
		if err != nil {
			return err
		}
		// Workspace state changes are guarded again under configstore's lock.
		// Checking here also avoids returning a misleading impact error for
		// an already-stale ETag.
		if fmt.Sprintf("%s-%d", current.ManagementIncarnation(), current.ManagementRevision()) != expected {
			return configstore.ErrStaleRevision
		}
		if !current.HasService(kind) {
			review := serviceActivationReview(current, kind, accessRevision, principals, publicationRevision, published)
			if review.RequiresConfirmation && digest == "" {
				return ErrServiceReviewRequired
			}
			if digest != "" && digest != review.Digest {
				return ErrServiceReviewChanged
			}
		} else if digest != "" {
			// A reviewed activation cannot silently turn into an unrelated
			// idempotent no-op. The caller must read current desired state.
			return ErrServiceReviewChanged
		}
		committed, err = s.store.SetWorkspaceServiceChecked(ctx, slug, kind, true, expected, authorize)
		return err
	})
	if errors.Is(err, ErrServiceReviewRequired) || errors.Is(err, ErrServiceReviewChanged) || errors.Is(err, configstore.ErrStaleRevision) {
		return Workspace{}, err
	}
	if err != nil {
		return Workspace{}, commandError(err)
	}
	return projectWorkspace(committed), nil
}

// withServiceAuthoritySnapshot acquires the same owner order as reviewed MCP
// mounting: publication, Access, then routing. The callback must not reenter
// the Access/publication stores and may hold their read locks until commit.
func (s Service) withServiceAuthoritySnapshot(project func(string, []accessstate.Access, string, []string) error) error {
	withAccess := func(publicationRevision string, published []string) error {
		if s.access == nil {
			return project("", nil, publicationRevision, published)
		}
		return s.access.WithReviewSnapshot(func(accessRevision string, current []accessstate.Access) error {
			return project(accessRevision, current, publicationRevision, published)
		})
	}
	if s.publications == nil {
		return withAccess("", nil)
	}
	return s.publications.WithReviewSnapshot(func(publicationRevision string, published []string) error {
		return withAccess(publicationRevision, published)
	})
}

func serviceActivationReview(workspace routing.Workspace, kind routing.ServiceKind, accessRevision string, principals []accessstate.Access, publicationRevision string, publications []string) ServiceActivationReview {
	slug := workspace.Slug()
	review := ServiceActivationReview{
		Service: string(kind), WorkspaceETag: fmt.Sprintf("%s-%d", workspace.ManagementIncarnation(), workspace.ManagementRevision()),
		AccessRevision: accessRevision, PublicationRevision: publicationRevision, Published: slices.Contains(publications, slug.String()),
		AffectedPrincipals: []ServiceAffectedPrincipal{},
	}
	now := time.Now()
	for _, principal := range principals {
		if principal.AllowsService(slug, kind, now) {
			review.AffectedPrincipals = append(review.AffectedPrincipals, ServiceAffectedPrincipal{ID: principal.ID.String(), Label: principal.Name})
		}
	}
	sort.Slice(review.AffectedPrincipals, func(i, j int) bool { return review.AffectedPrincipals[i].ID < review.AffectedPrincipals[j].ID })
	review.RequiresConfirmation = review.Published || len(review.AffectedPrincipals) > 0
	if review.RequiresConfirmation {
		review.Warning = "Enabling this Service also enables its future configuration for the listed current grants and published Workspace. Review the affected Principals and reachability before committing."
	}
	raw, _ := json.Marshal(review)
	fingerprint := sha256.Sum256(raw)
	review.Digest = hex.EncodeToString(fingerprint[:])
	return review
}
