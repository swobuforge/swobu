package workspace_delete

import (
	"context"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ports"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
)

type DeleteFunc func(context.Context, ports.DeleteWorkspaceRequest) error

type ConfirmationView struct {
	Workspace readmodel.WorkspaceReadModel
	Delete    DeleteFunc
	OnDeleted func(readmodel.WorkspaceID)
	OnArm     func(readmodel.WorkspaceID)
	OnCancel  func(readmodel.WorkspaceID)

	PendingDeleteWorkspaceID *tui.State[readmodel.WorkspaceID]
	row                      *ui.ConfirmActionRow
}

func Confirmation(workspace readmodel.WorkspaceReadModel, delete DeleteFunc, onDeleted func(readmodel.WorkspaceID)) *ConfirmationView {
	c := &ConfirmationView{
		Workspace:                 workspace,
		Delete:                    delete,
		OnDeleted:                 onDeleted,
		PendingDeleteWorkspaceID:  tui.NewState(readmodel.WorkspaceID("")),
	}
	c.row = ui.NewConfirmActionRow(c.rowID(), c.rowCopy(), c.confirmDelete)
	c.row.OnArm = func() {
		if c.OnArm != nil {
			c.OnArm(c.pendingWorkspaceID())
		}
	}
	c.row.OnCancel = func() {
		if c.OnCancel != nil {
			c.OnCancel(c.PendingDeleteWorkspaceID.Get())
		}
		c.PendingDeleteWorkspaceID.Set("")
	}
	return c
}

func (c *ConfirmationView) rowID() string {
	id := "workspace-delete"
	if c.Workspace.ID != "" {
		id += ":" + string(c.Workspace.ID)
	} else if c.Workspace.Slug != "" {
		id += ":" + c.Workspace.Slug
	}
	return id
}

func (c *ConfirmationView) pendingWorkspaceID() readmodel.WorkspaceID {
	if id := c.PendingDeleteWorkspaceID.Get(); id != "" {
		return id
	}
	return c.Workspace.ID
}

func (c *ConfirmationView) confirmationSlug() string {
	if c.PendingDeleteWorkspaceID.Get() != "" {
		return string(c.PendingDeleteWorkspaceID.Get())
	}
	return c.Workspace.Slug
}

func (c *ConfirmationView) rowCopy() ui.ConfirmActionCopy {
	return ui.ConfirmActionCopy{
		Label:           "delete",
		IdleValue:       "workspace",
		IdleAction:      "delete ↵",
		ConfirmValue:    "delete " + c.confirmationSlug() + "?",
		ConfirmAction:   "confirm ↵",
		SubmittingValue: "deleting " + c.confirmationSlug() + "…",
		SubmittingHint:  "wait",
		FailedValue:     "delete failed",
		FailedAction:    "retry ↵",
	}
}

func (c *ConfirmationView) Request(workspaceID readmodel.WorkspaceID) {
	c.PendingDeleteWorkspaceID.Set(workspaceID)
	c.row.SetCopy(c.rowCopy())
	c.row.OpenConfirm()
}

func (c *ConfirmationView) Back() bool {
	if !c.IsOpen() {
		return false
	}
	c.Cancel()
	return true
}

func (c *ConfirmationView) Cancel() { c.row.Cancel() }

func (c *ConfirmationView) KeyMap() tui.KeyMap {
	return ui.BackScope(c.IsOpen, c.Cancel)
}

func (c *ConfirmationView) confirmDelete() error {
	id := c.pendingWorkspaceID()
	if c.Delete == nil {
		return errWorkspaceDeleteNotWired
	}
	if err := c.Delete(context.Background(), ports.DeleteWorkspaceRequest{ID: id}); err != nil {
		return err
	}
	if c.OnDeleted != nil {
		c.OnDeleted(id)
	}
	c.Close()
	return nil
}

func (c *ConfirmationView) Close() {
	c.PendingDeleteWorkspaceID.Set("")
	if c.row == nil {
		return
	}
	c.row.OnCancel = nil
	c.row.Cancel()
	c.row.OnCancel = func() {
		if c.OnCancel != nil {
			c.OnCancel(c.PendingDeleteWorkspaceID.Get())
		}
		c.PendingDeleteWorkspaceID.Set("")
	}
}

func (c *ConfirmationView) IsOpen() bool {
	return c.row != nil && c.row.IsOpen()
}

func (c *ConfirmationView) Activate() {
	if !c.IsOpen() {
		c.Request(c.Workspace.ID)
		return
	}
	c.row.Confirm()
}

// Confirm runs the delete directly. Kept for callers and tests that drive the
// confirmation without a mounted key event.
func (c *ConfirmationView) Confirm(context.Context) {
	if !c.IsOpen() {
		return
	}
	c.row.Confirm()
}

func ConfirmationRowComponent(c *ConfirmationView) *ui.ConfirmActionRow {
	if c.row == nil {
		c.row = ui.NewConfirmActionRow(c.rowID(), c.rowCopy(), c.confirmDelete)
	}
	c.row.SetCopy(c.rowCopy())
	return c.row
}

templ (c *ConfirmationView) Render() {
	<div class="flex-col w-full">
		@ConfirmationRowComponent(c)
	</div>
}

var errWorkspaceDeleteNotWired = errString("workspace delete is not wired yet")

type errString string

func (e errString) Error() string { return string(e) }
