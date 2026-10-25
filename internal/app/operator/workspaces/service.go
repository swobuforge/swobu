package workspaces

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/accounting"
	"github.com/swobuforge/swobu/internal/app/operator/routebindings"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/publications"
	"github.com/swobuforge/swobu/internal/routing"
)

var ErrMCPMountReviewChanged = errors.New("MCP mount review changed")

type Service struct {
	store        *configstore.Store
	connections  *connections.Store
	access       *accessstate.Store
	bindings     *routebindings.Coordinator
	publications *publications.Store
	accounting   *accounting.Store
}

// MCPConnectionReview is the non-secret metadata used to review a mount.
// Identity and permission declarations are operator-supplied, not verified.
type MCPConnectionReview struct {
	ID                string
	Endpoint          string
	ExecutionIdentity string
	PermissionSummary string
	Revision          string
}

// Dependencies names the independent owners required by Workspace operations.
// A constructor cannot silently ignore an unknown or repeated dependency.
// Small read-only and setup operations may omit owners they never use.
type Dependencies struct {
	Connections  *connections.Store
	Access       *accessstate.Store
	Bindings     *routebindings.Coordinator
	Publications *publications.Store
	Accounting   *accounting.Store
}

func NewService(store *configstore.Store, deps Dependencies) (Service, error) {
	if store == nil {
		return Service{}, errors.New("workspace store is required")
	}
	return Service{
		store:        store,
		connections:  deps.Connections,
		access:       deps.Access,
		bindings:     deps.Bindings,
		publications: deps.Publications,
		accounting:   deps.Accounting,
	}, nil
}

type CreateWorkspace struct {
	Slug         string      `json:"slug"`
	DisplayName  string      `json:"display_name,omitempty"`
	InitialRoute string      `json:"initial_route,omitempty"`
	Target       TargetDraft `json:"target,omitempty"`
}
type ReplaceRoute struct {
	Workspace, Route string
	Spec             RouteSpec
}

// MCPMountDraft describes a Workspace-local alias and selected tools backed by
// one reusable Connection. It grants no authority until reviewed checked apply.
type MCPMountDraft struct {
	Workspace     string
	Alias         string
	ConnectionID  string
	Builtin       string
	SelectedTools []string
}

// ApplyMCPMountChecked commits one exact mount reviewed by
// PreviewMCPMount while holding the affected owner locks in a stable
// order. Caller-owned confirmation is represented by the digest supplied from
// the reviewed preview; this method re-derives it before publication.
func (s Service) ApplyMCPMountChecked(ctx context.Context, command MCPMountDraft, review MCPMountReviewRequest, authorize func(context.Context) error) (Workspace, error) {
	connection, err := s.reviewMCPSource(command)
	if err != nil {
		return Workspace{}, err
	}
	if s.store == nil {
		return Workspace{}, CommandError{Unavailable, "Connection review is unavailable"}
	}
	slug, err := routing.ParseWorkspaceSlug(command.Workspace)
	if err != nil || slug.String() != command.Workspace {
		return Workspace{}, CommandError{InvalidArgument, "exact Workspace required"}
	}
	if err := routing.ValidateMCPMountAlias(command.Alias); err != nil {
		return Workspace{}, commandError(err)
	}
	var validTools bool
	command.SelectedTools, validTools = sortedUnique(command.SelectedTools)
	if !validTools {
		return Workspace{}, CommandError{InvalidArgument, "MCP tool allowlist must contain unique trimmed names"}
	}
	var result Workspace
	if s.publications != nil {
		unlock := s.publications.LockLifecycle()
		defer unlock()
	}
	if s.bindings != nil {
		unlock := s.bindings.Lock()
		defer unlock()
	}
	err = func() error {
		if connection.Revision != review.ConnectionRevision {
			return configstore.ErrMCPMountReviewConflict
		}
		apply := func(accessVersion string, identities []accessstate.Access, publicationVersion string, bindings []string) error {
			guard := func(reviewedWorkspace routing.Workspace, publish func() error) error {
				preview := buildMCPMountPreview(reviewedWorkspace, command, connection, accessVersion, identities, publicationVersion, bindings)
				if preview.Digest != review.Digest || preview.Revision != review.WorkspaceETag || preview.AccessRevision != review.AccessRevision || preview.PublicationRevision != review.PublicationRevision {
					return configstore.ErrMCPMountReviewConflict
				}
				return publish()
			}
			workspace, applyErr := s.store.ApplyMCPMountChecked(ctx, slug, command.Alias, routing.MCPMount{Builtin: command.Builtin, ConnectionID: command.ConnectionID, AllowedTools: append([]string(nil), command.SelectedTools...)}, review.WorkspaceETag, review.ConnectionRevision, authorize, guard)
			if applyErr != nil {
				return applyErr
			}
			result = projectWorkspace(workspace)
			return nil
		}
		withAccess := func(publicationVersion string, bindings []string) error {
			if s.access == nil {
				return apply("", nil, publicationVersion, bindings)
			}
			return s.access.WithReviewSnapshot(func(version string, current []accessstate.Access) error {
				return apply(version, current, publicationVersion, bindings)
			})
		}
		if s.publications == nil {
			return withAccess("", nil)
		}
		return s.publications.WithReviewSnapshot(func(version string, current []string) error {
			return withAccess(version, current)
		})
	}()
	if errors.Is(err, configstore.ErrStaleRevision) || errors.Is(err, configstore.ErrMCPMountReviewConflict) || errors.Is(err, accessstate.ErrRevisionConflict) || errors.Is(err, publications.ErrConflict) {
		return Workspace{}, errors.Join(CommandError{Conflict, "MCP mount review changed; obtain a fresh preview"}, ErrMCPMountReviewChanged)
	}
	if err != nil {
		return Workspace{}, commandError(err)
	}
	return result, nil
}

// sortedUnique turns the reviewed tool set into deterministic intent and
// rejects empty/duplicate names before they can be presented as distinct tools.
func sortedUnique(values []string) ([]string, bool) {
	out := append([]string(nil), values...)
	sort.Strings(out)
	for index, value := range out {
		if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) || (index > 0 && value == out[index-1]) {
			return nil, false
		}
	}
	return out, true
}

// PreviewMCPMount derives an effect-free source, grant, publication and
// mount summary from current owners. All selected tools remain potentially
// effectful because remote annotations and descriptions are untrusted.
func (s Service) PreviewMCPMount(ctx context.Context, command MCPMountDraft) (MCPMountPreview, error) {
	connection, err := s.reviewMCPSource(command)
	if err != nil {
		return MCPMountPreview{}, err
	}
	if s.store == nil {
		return MCPMountPreview{}, CommandError{Unavailable, "Connection review is unavailable"}
	}
	slug, err := routing.ParseWorkspaceSlug(command.Workspace)
	if err != nil || slug.String() != command.Workspace {
		return MCPMountPreview{}, CommandError{InvalidArgument, "exact Workspace required"}
	}
	if err := routing.ValidateMCPMountAlias(command.Alias); err != nil {
		return MCPMountPreview{}, commandError(err)
	}
	var validTools bool
	command.SelectedTools, validTools = sortedUnique(command.SelectedTools)
	if !validTools {
		return MCPMountPreview{}, CommandError{InvalidArgument, "MCP tool allowlist must contain unique trimmed names"}
	}
	var preview MCPMountPreview
	err = func() error {
		build := func(accessVersion string, identities []accessstate.Access, publicationVersion string, bindings []string) error {
			workspace, err := s.store.GetWorkspace(ctx, slug)
			if err != nil {
				return commandError(err)
			}
			if workspace.ManagementRevision() > (1<<53)-1 {
				return CommandError{Unavailable, "Workspace management revision exceeds exact JSON integer support"}
			}
			if _, err := routing.NewConfig([]routing.Workspace{workspace}); err != nil {
				return commandError(err)
			}
			preview = buildMCPMountPreview(workspace, command, connection, accessVersion, identities, publicationVersion, bindings)
			return nil
		}
		if s.publications == nil {
			if s.access == nil {
				return build("", nil, "", nil)
			}
			return s.access.WithReviewSnapshot(func(revision string, identities []accessstate.Access) error {
				return build(revision, identities, "", nil)
			})
		}
		return s.publications.WithReviewSnapshot(func(publicationVersion string, bindings []string) error {
			if s.access == nil {
				return build("", nil, publicationVersion, bindings)
			}
			return s.access.WithReviewSnapshot(func(revision string, identities []accessstate.Access) error {
				return build(revision, identities, publicationVersion, bindings)
			})
		})
	}()
	if err != nil {
		return MCPMountPreview{}, commandError(err)
	}
	return preview, nil
}

// reviewMCPSource admits the sole immutable server-owned source explicitly.
// No Connection name, endpoint, or operator declaration selects this variant.
func (s Service) reviewMCPSource(command MCPMountDraft) (MCPConnectionReview, error) {
	if command.Builtin == "" {
		return s.reviewMCPConnection(command.ConnectionID)
	}
	if command.Builtin != routing.AdminMCPSource || command.ConnectionID != "" {
		return MCPConnectionReview{}, CommandError{InvalidArgument, "select exactly one supported builtin or Connection"}
	}
	return MCPConnectionReview{ID: "builtin:" + routing.AdminMCPSource, Revision: routing.AdminMCPRevision,
		ExecutionIdentity: "This Swobu Node", PermissionSummary: "Full Node administration through canonical Control commands"}, nil
}

// reviewMCPConnection reads the one canonical reusable identity. No separate
// source registry or caller-supplied metadata callback can override it.
func (s Service) reviewMCPConnection(raw string) (MCPConnectionReview, error) {
	id, err := connectionidentity.Parse(raw)
	if err != nil || id.String() != raw {
		return MCPConnectionReview{}, CommandError{InvalidArgument, "exact MCP Connection ID required"}
	}
	if s.connections == nil {
		return MCPConnectionReview{}, CommandError{Unavailable, "Connection review is unavailable"}
	}
	value, err := s.connections.Get(id)
	if errors.Is(err, connections.ErrNotFound) {
		return MCPConnectionReview{}, CommandError{NotFound, "MCP Connection is unavailable"}
	}
	if err != nil {
		return MCPConnectionReview{}, commandError(err)
	}
	connection := MCPConnectionReview{ID: value.ID().String(), Revision: value.Revision().String()}
	switch spec := value.Spec().(type) {
	case connections.MCPHTTPSpec:
		connection.Endpoint = spec.Endpoint().String()
		connection.ExecutionIdentity = spec.ExecutionIdentity()
		connection.PermissionSummary = spec.PermissionSummary()
	case connections.MCPStdioSpec:
		connection.ExecutionIdentity = spec.ExecutionIdentity()
		connection.PermissionSummary = spec.PermissionSummary()
	default:
		return MCPConnectionReview{}, CommandError{InvalidArgument, "MCP Connection required"}
	}
	if connection.ExecutionIdentity == "" || connection.PermissionSummary == "" {
		return MCPConnectionReview{}, CommandError{InvalidArgument, "MCP execution identity and permission summary must be declared before mounting"}
	}
	return connection, nil
}

func buildMCPMountPreview(workspace routing.Workspace, command MCPMountDraft, connection MCPConnectionReview, accessVersion string, identities []accessstate.Access, publicationVersion string, bindings []string) MCPMountPreview {
	slug := workspace.Slug()
	allowed := append([]string{}, command.SelectedTools...)
	preview := MCPMountPreview{
		Workspace: slug.String(), Alias: command.Alias, Builtin: command.Builtin, ConnectionID: command.ConnectionID, Endpoint: connection.Endpoint,
		ExecutionIdentity: connection.ExecutionIdentity, IdentityVerified: false,
		PermissionSummary: connection.PermissionSummary, AllowedTools: allowed,
		PotentiallyEffectfulTools: append([]string{}, allowed...),
		AffectedPrincipals:        make([]MCPMountPrincipal, 0),
		Published:                 false,
		Revision:                  fmt.Sprintf("%s-%d", workspace.ManagementIncarnation(), workspace.ManagementRevision()),
		AccessRevision:            accessVersion, PublicationRevision: publicationVersion, ConnectionRevision: connection.Revision,
		ApprovalClass: "trusted_confirmation",
		Warning:       "Preview is not approval. The upstream identity and permission summary are operator-declared and unverified; remote tool effect classifications are untrusted. Review every selected tool as potentially effectful.",
	}
	if mounts := workspace.MCPMounts(); mounts[command.Alias].SourceID() != "" {
		current := mounts[command.Alias]
		preview.ExistingMount = &MCPMount{Alias: command.Alias, Builtin: current.Builtin, ConnectionID: current.ConnectionID, AllowedTools: append([]string(nil), current.AllowedTools...)}
	}
	now := time.Now()
	for _, identity := range identities {
		if identity.AllowsService(slug, routing.ServiceMCP, now) {
			preview.AffectedPrincipals = append(preview.AffectedPrincipals, MCPMountPrincipal{ID: identity.ID.String(), Label: identity.Name})
		}
	}
	sort.Slice(preview.AffectedPrincipals, func(i, j int) bool { return preview.AffectedPrincipals[i].ID < preview.AffectedPrincipals[j].ID })
	for _, published := range bindings {
		if published == slug.String() {
			preview.Published = true
			break
		}
	}
	if command.Builtin != "" {
		preview.IdentityVerified = true
		preview.Warning = "Delegates Node administration to every listed Principal with MCP access, including broad Workspace grants. Prefer a dedicated MCP-only Workspace."
	}
	preview.Digest = mcpMountPreviewDigest(preview)
	return preview
}

func mcpMountPreviewDigest(preview MCPMountPreview) string {
	preview.Digest = ""
	encoded, _ := json.Marshal(preview)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
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
		out = append(out, WorkspaceSummary{DisplayName: workspace.DisplayName(), Slug: workspace.Slug().String(), RouteCount: len(workspace.Routes())})
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
	if workspace.ManagementRevision() > (1<<53)-1 {
		return Workspace{}, CommandError{Unavailable, "Workspace management revision exceeds exact JSON integer support"}
	}
	return projectWorkspace(workspace), nil
}

// requireDeletableWorkspace is an explicit lifecycle precondition. Whole-Node
// admission forbids removing a Workspace while it still owns publications, so
// the old "delete routing, reconcile publications afterward" saga cannot work.
// Call while holding the publication lifecycle lock, BEFORE revoking bearers.
func (s Service) requireDeletableWorkspace(slug routing.WorkspaceSlug) error {
	if s.publications != nil {
		for _, publication := range s.publications.List() {
			if publication == slug.String() {
				return CommandError{Conflict, "Workspace is published; unpublish it before deletion"}
			}
		}
	}
	if s.accounting != nil {
		if _, configured := s.accounting.Limit(slug.String()); configured {
			return CommandError{Conflict, "Workspace has a spending policy; clear its limit before deleting it"}
		}
	}
	return nil
}

func (s Service) DeleteWorkspace(ctx context.Context, raw string) error {
	if s.store == nil {
		return CommandError{Unavailable, "store unavailable"}
	}
	slug, err := routing.ParseWorkspaceSlug(raw)
	if err != nil {
		return commandError(err)
	}
	if s.publications != nil {
		unlock := s.publications.LockLifecycle()
		defer unlock()
	}
	if err := s.requireDeletableWorkspace(slug); err != nil {
		return err
	}
	_, err = s.destructiveConfigUpdate(ctx, slug, nil, func(c routing.Config) (routing.Config, error) { return c.DeleteWorkspace(slug) })
	if err != nil {
		return commandError(err)
	}
	return nil
}

// DeleteWorkspaceChecked is the revision/replay-aware application boundary.
// Admission is repeated inside the routing transaction; publication blockers
// are checked before any Principal revocation is attempted.
func (s Service) DeleteWorkspaceChecked(ctx context.Context, raw, etag, scope, key string, authorize func(context.Context) error) (bool, error) {
	if s.store == nil {
		return false, CommandError{Unavailable, "store unavailable"}
	}
	slug, err := routing.ParseWorkspaceSlug(raw)
	if err != nil {
		return false, commandError(err)
	}
	if s.publications != nil {
		unlock := s.publications.LockLifecycle()
		defer unlock()
	}
	if s.bindings != nil {
		unlock := s.bindings.Lock()
		defer unlock()
	}
	if err := s.requireDeletableWorkspace(slug); err != nil {
		return false, err
	}
	replay, err := s.store.DeleteWorkspaceChecked(ctx, slug, etag, scope, key, authorize, func(routing.Config, routing.Config) error {
		if s.access != nil {
			_, revokeErr := s.access.RevokeWorkspace(slug)
			return revokeErr
		}
		return nil
	})
	if err != nil {
		return replay, err
	}
	return replay, nil
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

func (s Service) destructiveConfigUpdate(ctx context.Context, bindingWorkspace routing.WorkspaceSlug, route *routing.RouteName, edit func(routing.Config) (routing.Config, error)) (routing.Config, error) {
	if (route != nil || s.access == nil) || s.bindings == nil {
		return s.store.Update(ctx, edit)
	}
	unlock := s.bindings.Lock()
	defer unlock()
	return s.store.UpdatePrepared(ctx, edit, func(_, _ routing.Config) error {
		_, err := s.access.RevokeWorkspace(bindingWorkspace)
		return err
	})
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

// validateConnectionReference admits only present, kind-correct Connections.
// A Workspace attachment cannot fall back to a source name or a missing
// registry: both would create a configuration the Node cannot execute.
func (s Service) validateConnectionReference(raw string, want connections.Kind) error {
	if raw == "" || strings.TrimSpace(raw) != raw || !strings.HasPrefix(raw, "cn_") {
		return CommandError{InvalidArgument, "Workspace attachment requires a Connection ID"}
	}
	id, err := connectionidentity.Parse(raw)
	if err != nil {
		return CommandError{InvalidArgument, "invalid Connection ID"}
	}
	if s.connections == nil {
		return CommandError{Unavailable, "Connection registry is unavailable"}
	}
	value, err := s.connections.Get(id)
	if errors.Is(err, connections.ErrNotFound) {
		return CommandError{NotFound, "Connection not found"}
	}
	if err != nil {
		return commandError(err)
	}
	if value.Spec().Kind() != want {
		return CommandError{InvalidArgument, "Connection kind does not match Workspace attachment"}
	}
	return nil
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
	var existing CommandError
	if errors.As(err, &existing) {
		return existing
	}
	code := Internal
	switch {
	case errors.Is(err, configstore.ErrMCPMountReviewConflict), errors.Is(err, configstore.ErrStaleRevision), errors.Is(err, accessstate.ErrRevisionConflict), errors.Is(err, publications.ErrConflict):
		code = Conflict
	case errors.Is(err, routing.ErrServiceDisabled), errors.Is(err, routing.ErrInvalidConfig):
		code = InvalidArgument
	case errors.Is(err, connections.ErrUnknownProtocol), errors.Is(err, connections.ErrUnsupportedProtocol):
		code = InvalidArgument
	case errors.Is(err, routing.ErrNotFound):
		code = NotFound
	case errors.Is(err, routing.ErrServiceNotEmpty), errors.Is(err, routing.ErrConflict),
		errors.Is(err, routing.ErrLastTarget):
		code = Conflict
	case errors.Is(err, configstore.ErrStoreClosed):
		code = Unavailable
	}
	return CommandError{code, err.Error()}
}
