package workspace_delete

import (
	"context"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

type DeleteFunc func(context.Context, ports.DeleteWorkspaceRequest) error

type Phase int

const (
	PhaseClosed Phase = iota
	PhaseConfirming
	PhaseSubmitting
	PhaseFailed
)

type ConfirmationView struct {
	Workspace   readmodel.WorkspaceReadModel
	Phase       *tui.State[Phase]
	WorkspaceID *tui.State[readmodel.WorkspaceID]
	Error       *tui.State[string]
	Delete      DeleteFunc
	OnDeleted   func(readmodel.WorkspaceID)
}

func Confirmation(workspace readmodel.WorkspaceReadModel, delete DeleteFunc, onDeleted func(readmodel.WorkspaceID)) *ConfirmationView {
	return &ConfirmationView{
		Workspace:   workspace,
		Phase:       tui.NewState(PhaseClosed),
		WorkspaceID: tui.NewState(readmodel.WorkspaceID("")),
		Error:       tui.NewState(""),
		Delete:      delete,
		OnDeleted:   onDeleted,
	}
}

func (c *ConfirmationView) Request(workspaceID readmodel.WorkspaceID) {
	c.WorkspaceID.Set(workspaceID)
	c.Error.Set("")
	c.Phase.Set(PhaseConfirming)
}

func (c *ConfirmationView) Back() bool {
	if !c.IsOpen() {
		return false
	}
	c.Close()
	return true
}

func (c *ConfirmationView) Confirm(ctx context.Context) {
	if !c.IsOpen() {
		return
	}
	id := c.WorkspaceID.Get()
	if id == "" {
		id = c.Workspace.ID
	}
	if c.Delete == nil {
		c.Error.Set("workspace delete is not wired yet")
		c.Phase.Set(PhaseFailed)
		return
	}

	c.Error.Set("")
	c.Phase.Set(PhaseSubmitting)
	if err := c.Delete(ctx, ports.DeleteWorkspaceRequest{ID: id}); err != nil {
		c.Error.Set(err.Error())
		c.Phase.Set(PhaseFailed)
		return
	}
	if c.OnDeleted != nil {
		c.OnDeleted(id)
	}
	c.Close()
}

func (c *ConfirmationView) Close() {
	c.Error.Set("")
	c.Phase.Set(PhaseClosed)
}

func (c *ConfirmationView) IsOpen() bool {
	return c.Phase.Get() != PhaseClosed
}

func (c *ConfirmationView) Activate() {
	if !c.IsOpen() {
		c.Request(c.Workspace.ID)
		return
	}
	c.Confirm(context.Background())
}

func (c *ConfirmationView) confirmationSlug() string {
	if c.WorkspaceID.Get() != "" {
		return string(c.WorkspaceID.Get())
	}
	return c.Workspace.Slug
}

func (c *ConfirmationView) rowValue() string {
	if c.IsOpen() {
		return "delete " + c.confirmationSlug() + "?"
	}
	return "workspace"
}

func (c *ConfirmationView) actionLabel() string {
	if c.IsOpen() {
		return "confirm ↵"
	}
	return "delete ↵"
}

templ (c *ConfirmationView) Render() {
	<div class="flex-col w-full" deps={c.Phase, c.WorkspaceID, c.Error}>
		<div class="flex-row w-full focusable" onActivate={c.Activate}>
			<span class="w-5"></span>
			<span class="w-18">delete</span>
			<span class="w-36">{c.rowValue()}</span>
			<span>{c.actionLabel()}</span>
		</div>
		if c.Error.Get() != "" {
			<div class="flex-row w-full">
				<span class="w-9"></span>
				<span>{c.Error.Get()}</span>
			</div>
		}
	</div>
}
