package workspaces

import (
	"context"
	"errors"
	"strings"

	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/routing"
)

// CreationPreview is advisory, credential-free intent and readiness. It carries
// no lifetime/revision because the Workspace does not exist and reserves nothing.
type CreationPreview struct {
	Slug          string    `json:"slug"`
	DisplayName   string    `json:"display_name"`
	RouteCount    int       `json:"route_count"`
	TargetCount   int       `json:"target_count"`
	Readiness     Readiness `json:"readiness"`
	ApprovalClass string    `json:"approval_class"`
	Diff          string    `json:"diff"`
	Warning       string    `json:"warning"`
}

// PreviewCreation uses the apply draft constructor and store admission without
// publication. Connection and credential details never enter its projection.
func (s Service) PreviewCreation(ctx context.Context, cmd CreateWorkspace, authorize func(context.Context) error) (CreationPreview, error) {
	if s.store == nil {
		return CreationPreview{}, CommandError{Unavailable, "store unavailable"}
	}
	draft, err := creationDraft(cmd)
	if err != nil {
		return CreationPreview{}, commandError(err)
	}
	if err := s.store.PreviewWorkspaceCreation(ctx, draft, authorize); err != nil {
		if errors.Is(err, routing.ErrInvalidConfig) || errors.Is(err, routing.ErrConflict) {
			return CreationPreview{}, commandError(err)
		}
		return CreationPreview{}, err
	}
	projected := projectWorkspace(draft)
	count := 0
	for _, route := range draft.Routes() {
		for _, tier := range route.Tiers() {
			count += len(tier.Targets())
		}
	}
	return CreationPreview{Slug: projected.Slug, DisplayName: projected.DisplayName, RouteCount: len(projected.Routes), TargetCount: count, Readiness: projected.Readiness, ApprovalClass: "confirmation", Diff: "Create Workspace at immutable slug with the reviewed display metadata and initial routing configuration.", Warning: "Preview reserves nothing. Apply rechecks availability and authority; retain the same request and retry key for explicit recovery."}, nil
}

// creationDraft is the single validation path for empty and configured creation.
// It constructs semantic intent without publication or management authority.
func creationDraft(cmd CreateWorkspace) (routing.Workspace, error) {
	slug, err := routing.ParseWorkspaceSlug(cmd.Slug)
	if err != nil {
		// The route and target are request-owned input. Preserve the semantic
		// validation detail while exposing it through the operator API's stable
		// invalid-argument class.
		return routing.Workspace{}, CommandError{InvalidArgument, err.Error()}
	}
	var config routing.Config
	if strings.TrimSpace(cmd.InitialRoute) == "" {
		config, err = config.CreateEmptyWorkspace(routing.EmptyWorkspaceSeed{Slug: slug})
	} else {
		name, parseErr := routing.ParseRouteName(cmd.InitialRoute)
		if parseErr != nil {
			return routing.Workspace{}, parseErr
		}
		target, targetErr := cmd.Target.routingTarget()
		if targetErr != nil {
			return routing.Workspace{}, CommandError{InvalidArgument, targetErr.Error()}
		}
		config, err = config.CreateWorkspace(routing.WorkspaceSeed{Slug: slug, Route: name, Target: target})
	}
	if err != nil {
		return routing.Workspace{}, CommandError{InvalidArgument, err.Error()}
	}
	workspace, _ := config.Workspace(slug)
	return workspace.WithDisplayName(cmd.DisplayName)
}

// CreateWorkspaceChecked delegates atomic original-result replay to configstore.
// Scope and admission come from trusted callers, never request-body authority.
// Replay projects the original receipt result without querying current state.
func (s Service) CreateWorkspaceChecked(ctx context.Context, cmd CreateWorkspace, scope, key string, authorize func(context.Context) error) (Workspace, bool, error) {
	if s.store == nil {
		return Workspace{}, false, CommandError{Unavailable, "store unavailable"}
	}
	draft, err := creationDraft(cmd)
	if err != nil {
		return Workspace{}, false, commandError(err)
	}
	workspace, replay, err := s.store.CreateWorkspaceChecked(ctx, draft, scope, key, authorize)
	if err != nil {
		if errors.Is(err, routing.ErrInvalidConfig) || errors.Is(err, routing.ErrConflict) || errors.Is(err, routing.ErrNotFound) || errors.Is(err, connections.ErrUnknownProtocol) || errors.Is(err, connections.ErrUnsupportedProtocol) {
			return Workspace{}, false, commandError(err)
		}
		return Workspace{}, false, err
	}
	return projectWorkspace(workspace), replay, nil
}
