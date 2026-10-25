package workspaces

import (
	"context"
	"errors"

	"github.com/swobuforge/swobu/internal/routing"
)

// ModelMatch describes the saved model routing decision without invoking an
// upstream provider or revealing credentials. Positions are 1-based.
type ModelMatch struct {
	Matched  bool   `json:"matched"`
	Pattern  string `json:"pattern,omitempty"`
	Priority int    `json:"priority,omitempty"`
	Tiers    int    `json:"tiers,omitempty"`
	Targets  int    `json:"targets,omitempty"`
}

func (s Service) PreviewModelMatch(ctx context.Context, rawWorkspace, modelID string) (ModelMatch, error) {
	if s.store == nil {
		return ModelMatch{}, CommandError{Unavailable, "store unavailable"}
	}
	slug, err := routing.ParseWorkspaceSlug(rawWorkspace)
	if err != nil {
		return ModelMatch{}, commandError(err)
	}
	if modelID == "" {
		return ModelMatch{}, CommandError{InvalidArgument, "model ID is required"}
	}
	workspace, err := s.store.GetWorkspace(ctx, slug)
	if err != nil {
		return ModelMatch{}, commandError(err)
	}
	route, err := workspace.ResolveRoute(modelID)
	if errors.Is(err, routing.ErrNotFound) || errors.Is(err, routing.ErrNoRoute) || errors.Is(err, routing.ErrServiceDisabled) {
		return ModelMatch{}, nil
	}
	if err != nil {
		return ModelMatch{}, commandError(err)
	}
	out := ModelMatch{Matched: true, Pattern: route.Name().String(), Tiers: len(route.Tiers())}
	for i, configured := range workspace.Routes() {
		if configured.Name() == route.Name() {
			out.Priority = i + 1
			break
		}
	}
	for _, tier := range route.Tiers() {
		out.Targets += len(tier.Targets())
	}
	return out, nil
}
