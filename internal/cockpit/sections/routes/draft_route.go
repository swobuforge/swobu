package routes

import (
	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
)

// DraftRoute is the local add-route draft owned by the routes section. It is
// not a route-configuration component; the mounted draft rows below own render
// and activation.
type DraftRoute struct {
	WorkspaceID readmodel.WorkspaceID
	Expanded    *tui.State[bool]
	Name        *tui.State[string]
}

func NewDraftRoute(workspaceID readmodel.WorkspaceID) *DraftRoute {
	return &DraftRoute{
		WorkspaceID: workspaceID,
		Expanded:    tui.NewState(true),
		Name:        tui.NewState(""),
	}
}

func (d *DraftRoute) IsExpanded() bool {
	return d != nil && d.Expanded.Get()
}

func (d *DraftRoute) Open() {
	if d == nil {
		return
	}
	d.Expanded.Set(true)
}

func (d *DraftRoute) ModelName() string {
	if d == nil || d.Name == nil {
		return ""
	}
	return d.Name.Get()
}

func (d *DraftRoute) ContractValue() string {
	return "model = " + d.ModelName()
}
