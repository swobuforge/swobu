package readmodel

const ConventionalFirstWorkspaceSlug = "default"

// WorkspaceID is the stable Cockpit identifier for a workspace tab and body.
type WorkspaceID string

// WorkspaceReadModel is the shared screen snapshot for one Cockpit workspace.
//
// Workspace state is product projection data rendered by the workspace page. It
// includes only operator-facing projection fields; adapters translate concrete
// config, daemon, and client-profile state into this shape.
type WorkspaceReadModel struct {
	ID    WorkspaceID
	Slug  string
	State WorkspaceState
	// WorkspaceURL projects the canonical unversioned workspace URL. Boundary
	// sources may still supply a tolerated /v1 spelling; consumers normalize it
	// to workspace identity and never expose a second canonical base.
	WorkspaceURL    string
	Routes          []RouteReadModel
	Activity        ActivityReadModel
	ProviderOptions []ProviderOptionReadModel
}

// NewConventionalFirstWorkspace constructs the zero-persistence Cockpit
// projection for the first workspace. This is deliberately a read-model
// constructor: the daemon and routing domain never receive this state.
func NewConventionalFirstWorkspace(workspaceURL string, providerOptions []ProviderOptionReadModel) WorkspaceReadModel {
	return WorkspaceReadModel{
		ID:              WorkspaceID(ConventionalFirstWorkspaceSlug),
		Slug:            ConventionalFirstWorkspaceSlug,
		State:           WorkspaceBootstrap,
		WorkspaceURL:    workspaceURL,
		ProviderOptions: append([]ProviderOptionReadModel(nil), providerOptions...),
	}
}

// NewDraftWorkspace constructs the canonical [+] workspace projection.
// Provider options are ambient operator capabilities, not authored workspace
// setup, so callers supply the hydrated catalog separately from draft input.
func NewDraftWorkspace(providerOptions []ProviderOptionReadModel) WorkspaceReadModel {
	return WorkspaceReadModel{
		ID:              "+",
		State:           WorkspaceDraft,
		ProviderOptions: append([]ProviderOptionReadModel(nil), providerOptions...),
	}
}

// ResetDraftInput clears operator-authored onboarding state while preserving
// ambient capabilities required to start the next setup journey.
func (w WorkspaceReadModel) ResetDraftInput() WorkspaceReadModel {
	return NewDraftWorkspace(w.ProviderOptions)
}

// WorkspaceTabReadModel is the tab-rail projection for an existing,
// conventional-first, draft, or help tab.
type WorkspaceTabReadModel struct {
	ID       WorkspaceID
	Slug     string
	Kind     WorkspaceTabKind
	Selected bool
}

// WorkspaceTabKind classifies the fixed tab behaviors in the Cockpit rail.
type WorkspaceTabKind int

const (
	WorkspaceTabExisting WorkspaceTabKind = iota
	WorkspaceTabBootstrap
	WorkspaceTabDraft
	WorkspaceTabHelp
)

// WorkspaceState describes whether a workspace can render normal controls or
// must render local onboarding state.
type WorkspaceState int

const (
	WorkspaceExisting WorkspaceState = iota
	WorkspaceDraft
	// WorkspaceBootstrap is a Cockpit-only projection for zero persisted
	// workspaces. It must never cross into routing, configstore, or HTTP state.
	WorkspaceBootstrap
)

// IsDraft reports whether the workspace body is the [+] creation state.
func (w WorkspaceReadModel) IsDraft() bool {
	return w.State == WorkspaceDraft
}

// IsBootstrap reports whether Cockpit is projecting the conventional first
// workspace without a persisted workspace behind it.
func (w WorkspaceReadModel) IsBootstrap() bool {
	return w.State == WorkspaceBootstrap
}

// IsOnboarding reports whether route and target authoring remains local until
// the first target crosses the atomic workspace-creation boundary.
func (w WorkspaceReadModel) IsOnboarding() bool {
	return w.IsDraft() || w.IsBootstrap()
}

// IsPersisted reports whether the workspace may be queried for durable
// activity or other daemon-owned state.
func (w WorkspaceReadModel) IsPersisted() bool {
	return w.State == WorkspaceExisting
}

// RoutingWorkspaceID returns the namespace that draft route/target commands
// will create atomically. The [+] ID remains the Cockpit draft identity; its
// validated name becomes the future routing identity only at the command edge.
func (w WorkspaceReadModel) RoutingWorkspaceID() WorkspaceID {
	if w.IsDraft() && w.Slug != "" {
		return WorkspaceID(w.Slug)
	}
	return w.ID
}

// HasRoutes reports whether the workspace can render route rows.
func (w WorkspaceReadModel) HasRoutes() bool {
	return len(w.Routes) > 0
}
