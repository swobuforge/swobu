package readmodel

// CockpitReadModel is the root snapshot loaded by the Cockpit shell.
//
// It is navigation/shared data: tabs select the active surface, while the
// selected workspace body carries the section data rendered by the workspace
// plane.
type CockpitReadModel struct {
	Tabs                []WorkspaceTabReadModel
	SelectedWorkspaceID WorkspaceID
	SelectedWorkspace   WorkspaceReadModel
	Help                HelpReadModel
	EnvironmentLabel    string
	Surface             CockpitSurface
}

// CockpitSurface selects the static top-level surface rendered by the shell.
type CockpitSurface int

const (
	CockpitWorkspaceSurface CockpitSurface = iota
	CockpitHelpSurface
)
