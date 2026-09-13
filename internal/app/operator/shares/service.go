package shares

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/app/operator/routebindings"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/sharestate"
	"github.com/swobuforge/swobu/internal/sharetransport"
)

type Service struct {
	configStore *configstore.Store
	shareStore  *sharestate.Store
	runtime     *sharetransport.OwnerRuntime
	bindings    *routebindings.Coordinator
}

type Result struct {
	ShareURL         string `json:"share_url"`
	OpenAIBaseURL    string `json:"openai_base_url"`
	AnthropicBaseURL string `json:"anthropic_base_url"`
	APIKey           string `json:"api_key"`
	ExpiresAt        string `json:"expires_at"`
}

type Summary struct {
	Workspace string  `json:"workspace"`
	Route     string  `json:"route,omitempty"`
	Hostname  string  `json:"hostname"`
	ExpiresAt *string `json:"expires_at"`
}

func NewService(configStore *configstore.Store, shareStore *sharestate.Store, runtime *sharetransport.OwnerRuntime, coordinators ...*routebindings.Coordinator) (Service, error) {
	if configStore == nil || shareStore == nil || runtime == nil {
		return Service{}, fmt.Errorf("share service dependencies are required")
	}
	service := Service{configStore: configStore, shareStore: shareStore, runtime: runtime}
	if len(coordinators) > 0 {
		service.bindings = coordinators[0]
	}
	return service, nil
}

func (s Service) Issue(ctx context.Context, rawRef string, expiry sharestate.Expiry) (Result, error) {
	ref, err := parseRef(rawRef)
	if err != nil {
		return Result{}, err
	}
	workspace, err := s.configStore.GetWorkspace(ctx, ref.workspace)
	if err != nil {
		return Result{}, fmt.Errorf("resolve shared workspace: %w", err)
	}
	if err := validateRef(workspace, ref, rawRef); err != nil {
		return Result{}, err
	}
	lease, err := s.runtime.EnsureReady(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("prepare shared endpoint: %w", err)
	}
	defer lease.Release()
	unlock := func() {}
	if s.bindings != nil {
		unlock = s.bindings.Lock()
	}
	defer unlock()
	workspace, err = s.configStore.GetWorkspace(ctx, ref.workspace)
	if err != nil {
		return Result{}, fmt.Errorf("resolve shared workspace: %w", err)
	}
	if err := validateRef(workspace, ref, rawRef); err != nil {
		return Result{}, err
	}
	grant, err := s.shareStore.Issue(ref.workspace, ref.route, expiry)
	if err != nil {
		return Result{}, err
	}
	return s.result(grant)
}

func (s Service) List() ([]Summary, error) {
	grants := s.shareStore.ActiveGrants()
	if len(grants) == 0 {
		return []Summary{}, nil
	}
	endpointID, err := s.shareStore.EndpointID()
	if err != nil {
		return nil, err
	}
	hostname := sharestate.Hostname(endpointID)
	summaries := make([]Summary, 0, len(grants))
	for _, grant := range grants {
		var expiresAt *string
		if !grant.ExpiresAt.IsZero() {
			formatted := grant.ExpiresAt.UTC().Format(time.RFC3339)
			expiresAt = &formatted
		}
		summaries = append(summaries, Summary{Workspace: grant.Workspace.String(), Route: grant.Route.String(), Hostname: hostname, ExpiresAt: expiresAt})
	}
	return summaries, nil
}

func (s Service) Reveal(rawRef string) (Result, error) {
	ref, err := parseRef(rawRef)
	if err != nil {
		return Result{}, err
	}
	for _, grant := range s.shareStore.ActiveGrants() {
		if grant.Workspace != ref.workspace || grant.Route != ref.route {
			continue
		}
		return s.result(grant)
	}
	return Result{}, fmt.Errorf("share %q is not active", rawRef)
}

func (s Service) result(grant sharestate.Grant) (Result, error) {
	endpointID, err := s.shareStore.EndpointID()
	if err != nil {
		return Result{}, err
	}
	host := "https://" + sharestate.Hostname(endpointID)
	expires := "never"
	if !grant.ExpiresAt.IsZero() {
		expires = grant.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return Result{ShareURL: host + "/#" + grant.Bearer, OpenAIBaseURL: host + "/v1", AnthropicBaseURL: host, APIKey: grant.Bearer, ExpiresAt: expires}, nil
}

func (s Service) Revoke(rawRef string) error {
	ref, err := parseRef(rawRef)
	if err != nil {
		return err
	}
	unlock := func() {}
	if s.bindings != nil {
		unlock = s.bindings.Lock()
	}
	defer unlock()
	if err := s.shareStore.Revoke(ref.workspace, ref.route); err != nil {
		return err
	}
	s.runtime.StopIfInactive()
	return nil
}

type shareRef struct {
	workspace routing.WorkspaceSlug
	route     routing.RouteName
}

func parseRef(raw string) (shareRef, error) {
	workspaceRaw, routeRaw, ok := strings.Cut(strings.TrimSpace(raw), "/")
	if strings.Contains(routeRaw, "/") {
		return shareRef{}, fmt.Errorf("share must use <workspace> or <workspace>/<route>")
	}
	workspace, err := routing.ParseWorkspaceSlug(workspaceRaw)
	if err != nil {
		return shareRef{}, err
	}
	if !ok {
		return shareRef{workspace: workspace}, nil
	}
	route, err := routing.ParseRouteName(routeRaw)
	if err != nil {
		return shareRef{}, err
	}
	return shareRef{workspace: workspace, route: route}, nil
}

func validateRef(workspace routing.Workspace, ref shareRef, raw string) error {
	if ref.route.String() != "" {
		if _, ok := workspace.Route(ref.route); !ok {
			return fmt.Errorf("shared route %q does not exist", raw)
		}
	}
	return nil
}
