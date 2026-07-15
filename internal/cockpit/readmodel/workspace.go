package readmodel

// WorkspaceID is the stable Cockpit identifier for a workspace tab and body.
type WorkspaceID string

// WorkspaceReadModel is the shared screen snapshot for one Cockpit workspace.
//
// Workspace state is navigation/shared data owned by the workspace plane. It
// includes only operator-facing projection fields; adapters translate concrete
// config, daemon, and client-profile state into this shape.
type WorkspaceReadModel struct {
	ID            WorkspaceID
	Slug          string
	State         WorkspaceState
	ClientBaseURL string
	RunCommands   []RunCommandReadModel
	Routes        []RouteReadModel
	Activity      ActivityReadModel
	View          WorkspaceViewState
}

// WorkspaceViewState is screen state for static section rendering.
//
// It belongs to the workspace plane, not a feature workflow: all fields describe
// what is visible or focused in the current snapshot.
type WorkspaceViewState struct {
	WorkspaceExpanded       bool
	WorkspaceSummaryOnly    bool
	RoutesExpanded          bool
	ActivityExpanded        bool
	ExpandedRouteID         RouteID
	ExpandedActivityID      ActivityID
	DeleteWorkspaceConfirm  bool
	WorkspaceConfirmationID WorkspaceID
}

// WorkspaceTabReadModel is the tab-rail projection for an existing, draft, or
// help tab.
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
	WorkspaceTabDraft
	WorkspaceTabHelp
)

// WorkspaceState describes whether a workspace can render normal controls or
// must render the draft creation shape.
type WorkspaceState int

const (
	WorkspaceExisting WorkspaceState = iota
	WorkspaceDraft
)

// IsDraft reports whether the workspace body is the [+] creation state.
func (w WorkspaceReadModel) IsDraft() bool {
	return w.State == WorkspaceDraft
}

// HasRoutes reports whether the workspace can render route rows.
func (w WorkspaceReadModel) HasRoutes() bool {
	return len(w.Routes) > 0
}
