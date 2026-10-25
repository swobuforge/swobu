// Package workspaces owns operator queries and semantic commands over the
// latest routing aggregate. It converts transport drafts to routing.TargetDraft
// without interpreting providers and never accepts caller-authored snapshots.
package workspaces

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
)

type Workspace struct {
	DisplayName        string                `json:"display_name"`
	ManagementETag     string                `json:"management_etag"`
	ManagementRevision uint64                `json:"management_revision"`
	Slug               string                `json:"slug"`
	Routes             []Route               `json:"routes"`
	Readiness          Readiness             `json:"readiness"`
	MCPMounts          []MCPMount            `json:"mcp_mounts,omitempty"`
	EnabledServices    []string              `json:"enabled_services"`
	ServiceLimits      routing.ServiceLimits `json:"service_limits"`
	// SearchConnectionID is the internal projection for the Search route handler.
	SearchConnectionID string   `json:"-"`
	SearchFallbackIDs  []string `json:"-"`
}

// ServiceState distinguishes an absent Service from one enabled but not yet
// configured. Readiness is derived from actual typed children.
type ServiceState struct {
	Kind             string                   `json:"kind"`
	Enabled          bool                     `json:"enabled"`
	Configured       bool                     `json:"configured"`
	ActivationReview *ServiceActivationReview `json:"activation_review,omitempty"`
}

// ServiceActivationReview is the immutable advisory impact returned by the
// current Service query. Digest binds both other authority owners and the
// Workspace ETag; only explicit caller confirmation permits expansion.
type ServiceActivationReview struct {
	Service              string                     `json:"service"`
	WorkspaceETag        string                     `json:"workspace_etag"`
	AccessRevision       string                     `json:"access_revision"`
	PublicationRevision  string                     `json:"publication_revision"`
	AffectedPrincipals   []ServiceAffectedPrincipal `json:"affected_principals"`
	Published            bool                       `json:"published"`
	RequiresConfirmation bool                       `json:"requires_confirmation"`
	Warning              string                     `json:"warning,omitempty"`
	Digest               string                     `json:"digest"`
}

type ServiceAffectedPrincipal struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type MCPMount struct {
	Builtin      string   `json:"builtin,omitempty"`
	Alias        string   `json:"alias"`
	ConnectionID string   `json:"connection_id"`
	AllowedTools []string `json:"allowed_tools"`
}

// MCPMountPreview is an advisory, exact-intent summary for a proposed mount.
// ExecutionIdentity and PermissionSummary are disclosed as operator-declared
// facts; this value does not independently authenticate the upstream account.
type MCPMountPreview struct {
	Builtin                   string              `json:"builtin,omitempty"`
	Workspace                 string              `json:"workspace"`
	Alias                     string              `json:"alias"`
	ConnectionID              string              `json:"connection_id"`
	Endpoint                  string              `json:"endpoint"`
	ExecutionIdentity         string              `json:"execution_identity"`
	IdentityVerified          bool                `json:"identity_verified"`
	PermissionSummary         string              `json:"permission_summary"`
	AllowedTools              []string            `json:"allowed_tools"`
	PotentiallyEffectfulTools []string            `json:"potentially_effectful_tools"`
	ExistingMount             *MCPMount           `json:"existing_mount"`
	AffectedPrincipals        []MCPMountPrincipal `json:"affected_principals"`
	Published                 bool                `json:"published"`
	Revision                  string              `json:"revision"`
	AccessRevision            string              `json:"access_revision"`
	PublicationRevision       string              `json:"publication_revision"`
	Digest                    string              `json:"digest"`
	ConnectionRevision        string              `json:"connection_revision"`
	ApprovalClass             string              `json:"approval_class"`
	Warning                   string              `json:"warning"`
}

// MCPMountPrincipal identifies one current managed Workspace-use permission.
type MCPMountPrincipal struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// MCPMountReviewRequest carries the exact reviewed apply contract. Digest binds
// the server preview; revisions bind every authority owner represented there.
type MCPMountReviewRequest struct {
	WorkspaceETag       string `json:"workspace_etag"`
	Digest              string `json:"digest"`
	ConnectionRevision  string `json:"connection_revision"`
	AccessRevision      string `json:"access_revision"`
	PublicationRevision string `json:"publication_revision"`
}

type Readiness struct {
	LLM    string  `json:"llm"`
	MCP    string  `json:"mcp"`
	Search *string `json:"search,omitempty"`
}
type WorkspaceSummary struct {
	DisplayName string `json:"display_name"`
	Slug        string `json:"slug"`
	RouteCount  int    `json:"route_count"`
}
type Route struct {
	Name  string `json:"name"`
	Tiers []Tier `json:"tiers"`
}

// RouteSpec is the complete editable state accepted and returned by the route
// resource. Route identity comes from the request path and daemon-owned target
// versions never cross this boundary.
type RouteSpec struct {
	Tiers []TierSpec `json:"tiers"`
}
type TierSpec struct {
	Targets []TargetDraft `json:"targets"`
}
type Tier struct {
	Targets []Target `json:"targets"`
}
type Target struct {
	ID           string `json:"id"`
	Model        string `json:"model"`
	Protocol     string `json:"protocol"`
	ConnectionID string `json:"connection_id"`
}

// TargetDraft selects a reusable Models Connection and target-local fields.
type TargetDraft struct {
	ID           string `json:"id"`
	Model        string `json:"model"`
	Protocol     string `json:"protocol"`
	ConnectionID string `json:"connection_id"`
}

func projectWorkspace(workspace routing.Workspace) Workspace {
	llm := "disabled"
	if workspace.HasService(routing.ServiceModels) {
		llm = "ready"
	}
	if workspace.HasService(routing.ServiceModels) && len(workspace.Routes()) == 0 {
		llm = "not_configured"
	}
	mcpReadiness := "disabled"
	if workspace.HasService(routing.ServiceMCP) {
		mcpReadiness = "not_configured"
	}
	if len(workspace.MCPMounts()) > 0 {
		mcpReadiness = "configured"
	}
	var searchReadiness *string
	if workspace.HasService(routing.ServiceSearch) {
		value := "not_configured"
		if workspace.SearchConnectionID() != "" {
			value = "configured"
		}
		searchReadiness = &value
	}
	out := Workspace{ManagementETag: fmt.Sprintf("%s-%d", workspace.ManagementIncarnation(), workspace.ManagementRevision()), ManagementRevision: workspace.ManagementRevision(), Slug: workspace.Slug().String(), Readiness: Readiness{LLM: llm, MCP: mcpReadiness, Search: searchReadiness}, SearchConnectionID: workspace.SearchConnectionID(), SearchFallbackIDs: workspace.SearchFallbackIDs(), EnabledServices: make([]string, 0), Routes: make([]Route, 0)}
	out.DisplayName = workspace.DisplayName()
	out.ServiceLimits = workspace.ServiceLimits()
	for _, kind := range workspace.EnabledServices() {
		out.EnabledServices = append(out.EnabledServices, string(kind))
	}
	for alias, mount := range workspace.MCPMounts() {
		out.MCPMounts = append(out.MCPMounts, MCPMount{Alias: alias, Builtin: mount.Builtin, ConnectionID: mount.ConnectionID, AllowedTools: append([]string(nil), mount.AllowedTools...)})
	}
	sort.Slice(out.MCPMounts, func(i, j int) bool { return out.MCPMounts[i].Alias < out.MCPMounts[j].Alias })
	for _, route := range workspace.Routes() {
		projected := Route{Name: route.Name().String()}
		for _, tier := range route.Tiers() {
			projectedTier := Tier{}
			for _, target := range tier.Targets() {
				projectedTier.Targets = append(projectedTier.Targets, projectTarget(target))
			}
			sort.Slice(projectedTier.Targets, func(i, j int) bool { return projectedTier.Targets[i].ID < projectedTier.Targets[j].ID })
			projected.Tiers = append(projected.Tiers, projectedTier)
		}
		out.Routes = append(out.Routes, projected)
	}
	return out
}

func projectTarget(target routing.Target) Target {
	return Target{ID: target.ID().String(), Model: target.Model().String(), Protocol: target.Protocol().String(), ConnectionID: target.ConnectionID().String()}
}

func projectRouteSpec(route routing.Route) RouteSpec {
	out := RouteSpec{Tiers: make([]TierSpec, len(route.Tiers()))}
	for i, tier := range route.Tiers() {
		for _, target := range tier.Targets() {
			out.Tiers[i].Targets = append(out.Tiers[i].Targets, TargetDraft{ID: target.ID().String(), Model: target.Model().String(), Protocol: target.Protocol().String(), ConnectionID: target.ConnectionID().String()})
		}
	}
	return out
}

func (s RouteSpec) routingSpec() (routing.RouteSpec, error) {
	out := routing.RouteSpec{Tiers: make([]routing.TierSpec, len(s.Tiers))}
	for tierIndex, tier := range s.Tiers {
		out.Tiers[tierIndex].Targets = make([]routing.TargetSpec, len(tier.Targets))
		for targetIndex, draft := range tier.Targets {
			target, err := draft.routingTarget()
			if err != nil {
				return routing.RouteSpec{}, fmt.Errorf("tiers[%d].targets[%d]: %w", tierIndex, targetIndex, err)
			}
			out.Tiers[tierIndex].Targets[targetIndex] = routing.TargetSpec{
				ID: target.ID(), Settings: routing.TargetSettings{Model: target.Model(), Protocol: target.Protocol(), ConnectionID: target.ConnectionID()},
			}
		}
	}
	return out, nil
}

func (t TargetDraft) routingTarget() (routing.Target, error) {
	return routing.FinalizeTarget(routing.TargetDraft{ID: t.ID, Model: t.Model, Protocol: t.Protocol, ConnectionID: t.ConnectionID}, profile.RoutingConstructionFacts())
}

func normalize(raw string) string { return strings.TrimSpace(raw) }

// MatchesRoute checks complete desired topology against effective readback using
// the same provider/profile authoring rules as apply. Tier order is priority;
// target order within one tier is immaterial. Versions never enter this contract.
func (spec RouteSpec) MatchesRoute(route Route) bool {
	if len(spec.Tiers) == 0 || len(spec.Tiers) != len(route.Tiers) {
		return false
	}
	seen := make(map[string]bool)
	for index, tier := range spec.Tiers {
		if len(tier.Targets) == 0 || len(tier.Targets) != len(route.Tiers[index].Targets) {
			return false
		}
		expected := make(map[string]Target)
		for _, draft := range tier.Targets {
			target, err := draft.routingTarget()
			if err != nil || seen[draft.ID] {
				return false
			}
			seen[draft.ID] = true
			expected[draft.ID] = projectTarget(target)
		}
		for _, target := range route.Tiers[index].Targets {
			wanted, exists := expected[target.ID]
			if !exists {
				return false
			}
			left, err := json.Marshal(wanted)
			right, rightErr := json.Marshal(target)
			if err != nil || rightErr != nil || !bytes.Equal(left, right) {
				return false
			}
			delete(expected, target.ID)
		}
		if len(expected) != 0 {
			return false
		}
	}
	return true
}
