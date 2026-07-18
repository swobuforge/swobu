package workspaces

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/routing"
)

type Service struct{ store *configstore.Store }

func NewService(store *configstore.Store) (Service, error) {
	if store == nil {
		return Service{}, fmt.Errorf("workspace store is required")
	}
	return Service{store: store}, nil
}

type CreateWorkspace struct {
	Slug         string      `json:"slug"`
	InitialRoute string      `json:"initial_route"`
	Target       TargetDraft `json:"target"`
}
type RenameWorkspace struct {
	Slug    string `json:"-"`
	NewSlug string `json:"new_slug"`
}
type CreateRoute struct {
	Workspace string      `json:"-"`
	Name      string      `json:"name"`
	Target    TargetDraft `json:"target"`
}
type RenameRoute struct {
	Workspace, Route string
	NewName          string `json:"new_name"`
}
type SetDefaultRoute struct {
	Workspace string `json:"-"`
	Route     string `json:"route"`
}
type DeleteRoute struct{ Workspace, Route, Replacement string }
type CreateTarget struct {
	Workspace, Route string
	Target           TargetDraft `json:"target"`
	Placement        Placement   `json:"placement"`
}
type UpdateTargetSettings struct {
	Workspace, Route, TargetID string
	Target                     TargetSettingsDraft `json:"target"`
}
type DeleteTarget struct{ Workspace, Route, TargetID string }
type SetCredential struct {
	Workspace, Route, TargetID string
	Credential                 string `json:"credential"`
}
type Placement struct {
	BalanceWith *string `json:"balance_with,omitempty"`
}

// ErrorCode classifies the action a command client can take after failure.
// Concrete business reasons belong in CommandError.Message, not new codes.
type ErrorCode string

const (
	InvalidArgument ErrorCode = "INVALID_ARGUMENT"
	NotFound        ErrorCode = "NOT_FOUND"
	Conflict        ErrorCode = "CONFLICT"
	Unavailable     ErrorCode = "UNAVAILABLE"
	Internal        ErrorCode = "INTERNAL"
)

// CommandError is the stable application error rendered by inbound operator
// adapters. Code is machine-readable; Message preserves the concrete cause.
type CommandError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

func (e CommandError) Error() string { return e.Message }

func (s Service) ListWorkspaces(context.Context) ([]WorkspaceSummary, error) {
	if s.store == nil {
		return nil, CommandError{Unavailable, "store unavailable"}
	}
	out := []WorkspaceSummary{}
	for _, workspace := range s.store.Config().Workspaces() {
		out = append(out, WorkspaceSummary{Slug: workspace.Slug().String(), DefaultRoute: workspace.DefaultRoute().String(), RouteCount: len(workspace.Routes())})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}
func (s Service) GetWorkspace(_ context.Context, raw string) (Workspace, error) {
	if s.store == nil {
		return Workspace{}, CommandError{Unavailable, "store unavailable"}
	}
	slug, err := routing.ParseWorkspaceSlug(raw)
	if err != nil {
		return Workspace{}, commandError(err)
	}
	workspace, ok := s.store.Config().Workspace(slug)
	if !ok {
		return Workspace{}, CommandError{NotFound, "workspace not found"}
	}
	return projectWorkspace(workspace), nil
}
func (s Service) CreateWorkspace(ctx context.Context, cmd CreateWorkspace) (Workspace, error) {
	slug, e := routing.ParseWorkspaceSlug(cmd.Slug)
	if e != nil {
		return Workspace{}, commandError(e)
	}
	name, e := routing.ParseRouteName(cmd.InitialRoute)
	if e != nil {
		return Workspace{}, commandError(e)
	}
	target, e := cmd.Target.routingTarget()
	if e != nil {
		return Workspace{}, commandError(e)
	}
	return s.update(ctx, slug, func(c routing.Config) (routing.Config, error) {
		return c.CreateWorkspace(routing.WorkspaceSeed{Slug: slug, Route: name, Target: target})
	})
}
func (s Service) RenameWorkspace(ctx context.Context, cmd RenameWorkspace) (Workspace, error) {
	from, e := routing.ParseWorkspaceSlug(cmd.Slug)
	if e != nil {
		return Workspace{}, commandError(e)
	}
	to, e := routing.ParseWorkspaceSlug(cmd.NewSlug)
	if e != nil {
		return Workspace{}, commandError(e)
	}
	return s.update(ctx, to, func(c routing.Config) (routing.Config, error) { return c.RenameWorkspace(from, to) })
}
func (s Service) DeleteWorkspace(ctx context.Context, raw string) error {
	if s.store == nil {
		return CommandError{Unavailable, "store unavailable"}
	}
	slug, e := routing.ParseWorkspaceSlug(raw)
	if e != nil {
		return commandError(e)
	}
	_, e = s.store.Update(ctx, func(c routing.Config) (routing.Config, error) { return c.DeleteWorkspace(slug) })
	return commandErrorOrNil(e)
}
func (s Service) CreateRoute(ctx context.Context, cmd CreateRoute) (Workspace, error) {
	slug, name, target, e := parseRouteTarget(cmd.Workspace, cmd.Name, cmd.Target)
	if e != nil {
		return Workspace{}, commandError(e)
	}
	return s.update(ctx, slug, func(c routing.Config) (routing.Config, error) { return c.CreateRoute(slug, name, target) })
}
func (s Service) RenameRoute(ctx context.Context, cmd RenameRoute) (Workspace, error) {
	slug, e := routing.ParseWorkspaceSlug(cmd.Workspace)
	if e != nil {
		return Workspace{}, commandError(e)
	}
	from, e := routing.ParseRouteName(cmd.Route)
	if e != nil {
		return Workspace{}, commandError(e)
	}
	to, e := routing.ParseRouteName(cmd.NewName)
	if e != nil {
		return Workspace{}, commandError(e)
	}
	return s.update(ctx, slug, func(c routing.Config) (routing.Config, error) { return c.RenameRoute(slug, from, to) })
}
func (s Service) SetDefaultRoute(ctx context.Context, cmd SetDefaultRoute) (Workspace, error) {
	slug, e := routing.ParseWorkspaceSlug(cmd.Workspace)
	if e != nil {
		return Workspace{}, commandError(e)
	}
	name, e := routing.ParseRouteName(cmd.Route)
	if e != nil {
		return Workspace{}, commandError(e)
	}
	return s.update(ctx, slug, func(c routing.Config) (routing.Config, error) { return c.SetDefaultRoute(slug, name) })
}
func (s Service) DeleteRoute(ctx context.Context, cmd DeleteRoute) (Workspace, error) {
	slug, e := routing.ParseWorkspaceSlug(cmd.Workspace)
	if e != nil {
		return Workspace{}, commandError(e)
	}
	name, e := routing.ParseRouteName(cmd.Route)
	if e != nil {
		return Workspace{}, commandError(e)
	}
	var replacement *routing.RouteName
	if normalize(cmd.Replacement) != "" {
		value, x := routing.ParseRouteName(cmd.Replacement)
		if x != nil {
			return Workspace{}, commandError(x)
		}
		replacement = &value
	}
	return s.update(ctx, slug, func(c routing.Config) (routing.Config, error) { return c.DeleteRoute(slug, name, replacement) })
}
func (s Service) CreateTarget(ctx context.Context, cmd CreateTarget) (Workspace, error) {
	slug, name, target, e := parseRouteTarget(cmd.Workspace, cmd.Route, cmd.Target)
	if e != nil {
		return Workspace{}, commandError(e)
	}
	balanceWith, e := parseBalanceWith(cmd.Placement)
	if e != nil {
		return Workspace{}, commandError(e)
	}
	return s.update(ctx, slug, func(c routing.Config) (routing.Config, error) { return c.CreateTarget(slug, name, target, balanceWith) })
}
func (s Service) UpdateTargetSettings(ctx context.Context, cmd UpdateTargetSettings) (Workspace, error) {
	slug, e := routing.ParseWorkspaceSlug(cmd.Workspace)
	if e != nil {
		return Workspace{}, commandError(e)
	}
	name, e := routing.ParseRouteName(cmd.Route)
	if e != nil {
		return Workspace{}, commandError(e)
	}
	target, e := cmd.Target.routingTarget(cmd.TargetID)
	if e != nil {
		return Workspace{}, commandError(e)
	}
	return s.update(ctx, slug, func(c routing.Config) (routing.Config, error) {
		return c.UpdateTargetSettings(slug, name, target.ID(), routing.TargetSettings{Model: target.Model(), Protocol: target.Protocol(), Connection: target.Connection()})
	})
}
func (s Service) DeleteTarget(ctx context.Context, cmd DeleteTarget) (Workspace, error) {
	slug, e := routing.ParseWorkspaceSlug(cmd.Workspace)
	if e != nil {
		return Workspace{}, commandError(e)
	}
	name, e := routing.ParseRouteName(cmd.Route)
	if e != nil {
		return Workspace{}, commandError(e)
	}
	id, e := routing.ParseTargetID(cmd.TargetID)
	if e != nil {
		return Workspace{}, commandError(e)
	}
	return s.update(ctx, slug, func(c routing.Config) (routing.Config, error) { return c.DeleteTarget(slug, name, id) })
}
func (s Service) SetCredential(ctx context.Context, cmd SetCredential) (Workspace, error) {
	slug, e := routing.ParseWorkspaceSlug(cmd.Workspace)
	if e != nil {
		return Workspace{}, commandError(e)
	}
	name, e := routing.ParseRouteName(cmd.Route)
	if e != nil {
		return Workspace{}, commandError(e)
	}
	id, e := routing.ParseTargetID(cmd.TargetID)
	if e != nil {
		return Workspace{}, commandError(e)
	}
	return s.update(ctx, slug, func(c routing.Config) (routing.Config, error) { return c.SetCredential(slug, name, id, cmd.Credential) })
}

func (s Service) update(ctx context.Context, slug routing.WorkspaceSlug, edit func(routing.Config) (routing.Config, error)) (Workspace, error) {
	if s.store == nil {
		return Workspace{}, CommandError{Unavailable, "store unavailable"}
	}
	next, e := s.store.Update(ctx, edit)
	if e != nil {
		return Workspace{}, commandError(e)
	}
	workspace, ok := next.Workspace(slug)
	if !ok {
		return Workspace{}, CommandError{Internal, "committed workspace missing"}
	}
	return projectWorkspace(workspace), nil
}
func parseRouteTarget(rawSlug, rawRoute string, target TargetDraft) (routing.WorkspaceSlug, routing.RouteName, routing.Target, error) {
	slug, e := routing.ParseWorkspaceSlug(rawSlug)
	if e != nil {
		return routing.WorkspaceSlug{}, routing.RouteName{}, routing.Target{}, e
	}
	name, e := routing.ParseRouteName(rawRoute)
	if e != nil {
		return routing.WorkspaceSlug{}, routing.RouteName{}, routing.Target{}, e
	}
	value, e := target.routingTarget()
	return slug, name, value, e
}
func parseBalanceWith(p Placement) (*routing.TargetID, error) {
	if p.BalanceWith == nil {
		return nil, nil
	}
	id, err := routing.ParseTargetID(*p.BalanceWith)
	if err != nil {
		return nil, err
	}
	return &id, nil
}
func commandErrorOrNil(err error) error {
	if err == nil {
		return nil
	}
	return commandError(err)
}
func commandError(err error) error {
	if err == nil {
		return nil
	}
	code := Internal
	switch {
	case errors.Is(err, routing.ErrInvalidConfig):
		code = InvalidArgument
	case errors.Is(err, routing.ErrNotFound):
		code = NotFound
	case errors.Is(err, routing.ErrConflict),
		errors.Is(err, routing.ErrLastTarget),
		errors.Is(err, routing.ErrLastRoute),
		errors.Is(err, routing.ErrDefaultReplacementRequired):
		code = Conflict
	case errors.Is(err, configstore.ErrStoreClosed):
		code = Unavailable
	}
	return CommandError{code, err.Error()}
}
