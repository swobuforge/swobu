package workspace_delete

import (
	"context"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
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
	Workspace readmodel.WorkspaceReadModel
	Phase     *tui.State[Phase]
	// PendingDeleteWorkspaceID is the local confirmation target. It survives the
	// arm -> confirm flow so the feature can delete the armed workspace even if
	// the parent snapshot rerenders underneath it.
	PendingDeleteWorkspaceID *tui.State[readmodel.WorkspaceID]
	Error                    *tui.State[string]
	Delete                   DeleteFunc
	OnDeleted                func(readmodel.WorkspaceID)
	// OnArm is called when the confirmation opens. Sink state here so the
	// parent can track that a cancellation is pending without holding a
	// persistent child reference.
	OnArm func(readmodel.WorkspaceID)
	// OnCancel is called before Close on Escape/Back so the parent can clear
	// its own pending-delete state in the same render cycle.
	OnCancel func(readmodel.WorkspaceID)
}

func Confirmation(workspace readmodel.WorkspaceReadModel, delete DeleteFunc, onDeleted func(readmodel.WorkspaceID)) *ConfirmationView {
	return &ConfirmationView{
		Workspace:               workspace,
		Phase:                   tui.NewState(PhaseClosed),
		PendingDeleteWorkspaceID: tui.NewState(readmodel.WorkspaceID("")),
		Error:                   tui.NewState(""),
		Delete:                  delete,
		OnDeleted:               onDeleted,
	}
}

func (c *ConfirmationView) Request(workspaceID readmodel.WorkspaceID) {
	c.PendingDeleteWorkspaceID.Set(workspaceID)
	c.Error.Set("")
	c.Phase.Set(PhaseConfirming)
	if c.OnArm != nil {
		c.OnArm(workspaceID)
	}
}

func (c *ConfirmationView) Back() bool {
	if !c.IsOpen() {
		return false
	}
	c.Cancel()
	return true
}

func (c *ConfirmationView) Cancel() {
	if c.OnCancel != nil {
		c.OnCancel(c.PendingDeleteWorkspaceID.Get())
	}
	c.Close()
}

func (c *ConfirmationView) KeyMap() tui.KeyMap {
	return nil
}

func (c *ConfirmationView) Confirm(ctx context.Context) {
	if !c.IsOpen() {
		return
	}
	id := c.PendingDeleteWorkspaceID.Get()
	// If the confirmation was armed from a fresh snapshot, the local state may
	// still be empty. Fall back to the parent snapshot rather than failing the
	// delete request.
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
	if c.PendingDeleteWorkspaceID.Get() != "" {
		return string(c.PendingDeleteWorkspaceID.Get())
	}
	return c.Workspace.Slug
}

func (c *ConfirmationView) RowValue() string {
	if c.IsOpen() {
		return "delete " + c.confirmationSlug() + "?"
	}
	return "workspace"
}

func (c *ConfirmationView) ActionLabel() string {
	if c.IsOpen() {
		return "confirm ↵"
	}
	return "delete ↵"
}

func ConfirmationRowComponent(c *ConfirmationView) *ui.SelectableRow {
	id := "workspace-delete"
	if c.Workspace.ID != "" {
		id += ":" + string(c.Workspace.ID)
	} else if c.Workspace.Slug != "" {
		id += ":" + c.Workspace.Slug
	}
	row := ui.NewSelectableRow(id, "delete", c.RowValue(), c.ActionLabel(), c.Activate)
	if c.IsOpen() {
		row.OnCancel = c.Cancel
	}
	return row
}

templ (c *ConfirmationView) Render() {
	<div class="flex-col w-full" deps={c.Phase, c.PendingDeleteWorkspaceID, c.Error}>
		@ConfirmationRowComponent(c)
		if c.Error.Get() != "" {
			<div class="flex-row w-full">
				<span class="w-9"></span>
				<span>{c.Error.Get()}</span>
			</div>
		}
	</div>
}
