package workspaces

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/swobuforge/swobu/internal/app/operator/routebindings"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/sharestate"
	"github.com/swobuforge/swobu/internal/sharetransport"
)

type Service struct {
	store    *configstore.Store
	shares   *sharestate.Store
	runtime  *sharetransport.OwnerRuntime
	bindings *routebindings.Coordinator
}

func NewService(store *configstore.Store, lifecycle ...any) (Service, error) {
	if store == nil {
		return Service{}, fmt.Errorf("workspace store is required")
	}
	service := Service{store: store}
	for _, dependency := range lifecycle {
		switch value := dependency.(type) {
		case *sharestate.Store:
			service.shares = value
		case *sharetransport.OwnerRuntime:
			service.runtime = value
		case *routebindings.Coordinator:
			service.bindings = value
		}
	}
	return service, nil
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
type ReplaceRoute struct {
	Workspace, Route string
	Spec             RouteSpec
}
type ApplyCredentialReference struct {
	Workspace, Route, TargetID string
	Credential                 string `json:"credential"`
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
	ws, e := s.update(ctx, slug, func(c routing.Config) (routing.Config, error) {
		return c.CreateWorkspace(routing.WorkspaceSeed{Slug: slug, Route: name, Target: target})
	})
	if e != nil {
		return ws, e
	}
	return ws, nil
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
	return s.destructiveUpdate(ctx, to, from, nil, func(c routing.Config) (routing.Config, error) { return c.RenameWorkspace(from, to) })
}
func (s Service) DeleteWorkspace(ctx context.Context, raw string) error {
	if s.store == nil {
		return CommandError{Unavailable, "store unavailable"}
	}
	slug, e := routing.ParseWorkspaceSlug(raw)
	if e != nil {
		return commandError(e)
	}
	_, e = s.destructiveConfigUpdate(ctx, slug, nil, func(c routing.Config) (routing.Config, error) { return c.DeleteWorkspace(slug) })
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
	return s.destructiveUpdate(ctx, slug, slug, &from, func(c routing.Config) (routing.Config, error) { return c.RenameRoute(slug, from, to) })
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
	return s.destructiveUpdate(ctx, slug, slug, &name, func(c routing.Config) (routing.Config, error) { return c.DeleteRoute(slug, name, replacement) })
}
func (s Service) GetRoute(_ context.Context, rawWorkspace, rawRoute string) (RouteSpec, error) {
	if s.store == nil {
		return RouteSpec{}, CommandError{Unavailable, "store unavailable"}
	}
	slug, err := routing.ParseWorkspaceSlug(rawWorkspace)
	if err != nil {
		return RouteSpec{}, commandError(err)
	}
	name, err := routing.ParseRouteName(rawRoute)
	if err != nil {
		return RouteSpec{}, commandError(err)
	}
	workspace, ok := s.store.Config().Workspace(slug)
	if !ok {
		return RouteSpec{}, CommandError{NotFound, "workspace not found"}
	}
	route, ok := workspace.Route(name)
	if !ok {
		return RouteSpec{}, CommandError{NotFound, "route not found"}
	}
	return projectRouteSpec(route), nil
}

func (s Service) ReplaceRoute(ctx context.Context, cmd ReplaceRoute) (Workspace, error) {
	slug, err := routing.ParseWorkspaceSlug(cmd.Workspace)
	if err != nil {
		return Workspace{}, commandError(err)
	}
	name, err := routing.ParseRouteName(cmd.Route)
	if err != nil {
		return Workspace{}, commandError(err)
	}
	desired, err := cmd.Spec.routingSpec()
	if err != nil {
		return Workspace{}, commandError(err)
	}
	if s.store == nil {
		return Workspace{}, CommandError{Unavailable, "store unavailable"}
	}
	next, err := s.store.Update(ctx, func(config routing.Config) (routing.Config, error) {
		return config.ApplyRouteSpec(slug, name, desired)
	})
	if err != nil {
		return Workspace{}, commandError(err)
	}
	workspace, ok := next.Workspace(slug)
	if !ok {
		return Workspace{}, CommandError{Internal, "committed workspace missing"}
	}
	return projectWorkspace(workspace), nil
}

// ApplyCredentialReference is the credential subsystem's narrow bridge into
// route reconciliation. Secret material is stored before this call; only its
// reference enters the desired route.
func (s Service) ApplyCredentialReference(ctx context.Context, cmd ApplyCredentialReference) (Workspace, error) {
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
	return s.update(ctx, slug, func(c routing.Config) (routing.Config, error) {
		return reconcileCredentialReference(c, slug, name, id, cmd.Credential)
	})
}

// revalidateCredentialUpdate preserves the typed routing edit while taking the
// replacement connection back through profile construction facts. Routing
// intentionally has no profile dependency, so the public application command
// owns this return to the catalog-validation boundary.
func reconcileCredentialReference(config routing.Config, slug routing.WorkspaceSlug, routeName routing.RouteName, id routing.TargetID, credential string) (routing.Config, error) {
	workspace, ok := config.Workspace(slug)
	if !ok {
		return routing.Config{}, fmt.Errorf("%w: workspace %q", routing.ErrNotFound, slug.String())
	}
	route, ok := workspace.Route(routeName)
	if !ok {
		return routing.Config{}, fmt.Errorf("%w: route %q", routing.ErrNotFound, routeName.String())
	}
	spec := route.Spec()
	for tierIndex := range spec.Tiers {
		for targetIndex := range spec.Tiers[tierIndex].Targets {
			target := &spec.Tiers[tierIndex].Targets[targetIndex]
			if target.ID != id {
				continue
			}
			connection, err := connectionWithCredential(target.Settings.Connection, credential)
			if err != nil {
				return routing.Config{}, err
			}
			target.Settings.Connection = connection
			return config.ApplyRouteSpec(slug, routeName, spec)
		}
	}
	return routing.Config{}, fmt.Errorf("%w: target %q", routing.ErrNotFound, id.String())
}

func connectionWithCredential(connection routing.Connection, credential string) (routing.Connection, error) {
	draft, err := connectionDraftFromRouting(connection)
	if err != nil {
		return nil, err
	}
	switch {
	case draft.Standard != nil:
		draft.Standard.Credential = credential
	case draft.ZAI != nil:
		draft.ZAI.Credential = credential
	case draft.Bedrock != nil:
		draft.Bedrock.Credential = credential
	case draft.Custom != nil && draft.Custom.Header != nil:
		draft.Custom.Header.Credential = credential
	case draft.Custom != nil:
		return nil, fmt.Errorf("connection.%s.credential: custom connection has no header auth", connection.Provider())
	default:
		return nil, fmt.Errorf("connection.%s.credential: connection does not carry a credential", connection.Provider())
	}
	return routing.FinalizeConnection(draft, profile.RoutingConstructionFacts())
}

func (s Service) destructiveConfigUpdate(ctx context.Context, bindingWorkspace routing.WorkspaceSlug, route *routing.RouteName, edit func(routing.Config) (routing.Config, error)) (routing.Config, error) {
	if s.shares == nil || s.bindings == nil {
		return s.store.Update(ctx, edit)
	}
	unlock := s.bindings.Lock()
	defer unlock()
	removed := false
	next, err := s.store.UpdatePrepared(ctx, edit, func(_, _ routing.Config) error {
		var revokeErr error
		removed, revokeErr = s.shares.RevokeBindings(bindingWorkspace, route)
		return revokeErr
	})
	if removed && s.runtime != nil {
		s.runtime.StopIfInactive()
	}
	return next, err
}

func (s Service) destructiveUpdate(ctx context.Context, resultSlug, bindingWorkspace routing.WorkspaceSlug, route *routing.RouteName, edit func(routing.Config) (routing.Config, error)) (Workspace, error) {
	next, err := s.destructiveConfigUpdate(ctx, bindingWorkspace, route, edit)
	if err != nil {
		return Workspace{}, commandError(err)
	}
	workspace, ok := next.Workspace(resultSlug)
	if !ok {
		return Workspace{}, CommandError{Internal, "committed workspace missing"}
	}
	return projectWorkspace(workspace), nil
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
