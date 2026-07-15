package workspace_delete

import (
	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

type ConfirmationView struct {
	Workspace   readmodel.WorkspaceReadModel
	Open        *tui.State[bool]
	WorkspaceID *tui.State[readmodel.WorkspaceID]
}

func Confirmation(workspace readmodel.WorkspaceReadModel) *ConfirmationView {
	return &ConfirmationView{
		Workspace:   workspace,
		Open:        tui.NewState(false),
		WorkspaceID: tui.NewState(readmodel.WorkspaceID("")),
	}
}

func (c *ConfirmationView) Request(workspaceID readmodel.WorkspaceID) {
	c.WorkspaceID.Set(workspaceID)
	c.Open.Set(true)
}

func (c *ConfirmationView) Back() bool {
	if !c.Open.Get() {
		return false
	}
	c.Open.Set(false)
	return true
}

func (c *ConfirmationView) confirmationSlug() string {
	if c.WorkspaceID.Get() != "" {
		return string(c.WorkspaceID.Get())
	}
	return c.Workspace.Slug
}

templ (c *ConfirmationView) Render() {
	<div class="flex-col w-full">
		if c.Open.Get() {
			@InertRow("delete workspace "+c.confirmationSlug()+"?", "", "y/n")
		}
	</div>
}

templ InertRow(label string, value string, action string) {
	<div class="flex-row w-full">
		<span class="w-5"></span>
		<span class="w-18">{label}</span>
		<span class="w-36">{value}</span>
		<span>{action}</span>
	</div>
}
