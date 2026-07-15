package readmodel

// CockpitReadModel is the root snapshot loaded by the Cockpit shell.
//
// It is navigation/shared data: tabs select the active page, while the
// selected workspace body carries the product data rendered by the workspace
// page.
type CockpitReadModel struct {
	Tabs                []WorkspaceTabReadModel
	SelectedWorkspaceID WorkspaceID
	SelectedWorkspace   WorkspaceReadModel
	Help                HelpReadModel
	EnvironmentLabel    string
	ActivePage          CockpitPage
}

// CockpitPage selects the top-level page rendered by the shell.
type CockpitPage int

const (
	CockpitWorkspacePage CockpitPage = iota
	CockpitHelpPage
)
