package shares

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/sharestate"
	"github.com/swobuforge/swobu/internal/sharetransport"
)

type Service struct {
	configStore *configstore.Store
	shareStore  *sharestate.Store
	runtime     *sharetransport.OwnerRuntime
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
	Route     string  `json:"route"`
	Hostname  string  `json:"hostname"`
	ExpiresAt *string `json:"expires_at"`
}

func NewService(configStore *configstore.Store, shareStore *sharestate.Store, runtime *sharetransport.OwnerRuntime) (Service, error) {
	if configStore == nil || shareStore == nil || runtime == nil {
		return Service{}, fmt.Errorf("share service dependencies are required")
	}
	return Service{configStore: configStore, shareStore: shareStore, runtime: runtime}, nil
}

func (s Service) Issue(ctx context.Context, routeRef string, expiry sharestate.Expiry) (Result, error) {
	workspaceSlug, routeName, err := parseRouteRef(routeRef)
	if err != nil {
		return Result{}, err
	}
	workspace, err := s.configStore.GetWorkspace(ctx, workspaceSlug)
	if err != nil {
		return Result{}, fmt.Errorf("resolve shared route: %w", err)
	}
	if _, ok := workspace.Route(routeName); !ok {
		return Result{}, fmt.Errorf("shared route %q does not exist", routeRef)
	}
	if err := s.runtime.EnsureReady(ctx); err != nil {
		return Result{}, fmt.Errorf("prepare shared endpoint: %w", err)
	}
	grant, err := s.shareStore.Issue(workspaceSlug, routeName, expiry)
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

func (s Service) Reveal(routeRef string) (Result, error) {
	workspace, route, err := parseRouteRef(routeRef)
	if err != nil {
		return Result{}, err
	}
	for _, grant := range s.shareStore.ActiveGrants() {
		if grant.Workspace != workspace || grant.Route != route {
			continue
		}
		return s.result(grant)
	}
	return Result{}, fmt.Errorf("shared route %q is not active", routeRef)
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

func (s Service) Revoke(routeRef string) error {
	workspace, route, err := parseRouteRef(routeRef)
	if err != nil {
		return err
	}
	if err := s.shareStore.Revoke(workspace, route); err != nil {
		return err
	}
	s.runtime.StopIfInactive()
	return nil
}

func parseRouteRef(raw string) (routing.WorkspaceSlug, routing.RouteName, error) {
	workspaceRaw, routeRaw, ok := strings.Cut(strings.TrimSpace(raw), "/")
	if !ok || strings.Contains(routeRaw, "/") {
		return routing.WorkspaceSlug{}, routing.RouteName{}, fmt.Errorf("route must use <workspace>/<route>")
	}
	workspace, err := routing.ParseWorkspaceSlug(workspaceRaw)
	if err != nil {
		return routing.WorkspaceSlug{}, routing.RouteName{}, err
	}
	route, err := routing.ParseRouteName(routeRaw)
	if err != nil {
		return routing.WorkspaceSlug{}, routing.RouteName{}, err
	}
	return workspace, route, nil
}
