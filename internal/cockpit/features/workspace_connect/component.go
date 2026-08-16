package workspace_connect

import (
	"net/url"
	"os"
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/clientconnect"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	cockpitui "github.com/swobuforge/swobu/internal/cockpit/ui"
)

type connectOperations interface {
	Discover(clientconnect.Target) []clientconnect.Client
	Plan(clientconnect.ClientID, clientconnect.Target) (clientconnect.Plan, error)
	Apply(clientconnect.Plan) error
}

// Disclosure owns the complete endpoint/client/API interaction lifecycle.
type Disclosure struct {
	Target       clientconnect.Target
	Ops          connectOperations
	Clients      *tui.State[[]clientconnect.Client]
	EndpointOpen *tui.State[bool]
	Plan         *tui.State[clientconnect.Plan]
	Error        *tui.State[string]
	OnNotice     func(readmodel.Notice)
}

// New constructs a disclosure. Discovery runs only when the operator opens it.
func New(target clientconnect.Target, ops connectOperations, onNotice func(readmodel.Notice)) *Disclosure {
	if ops == nil {
		ops = clientconnect.NewService()
	}
	return &Disclosure{
		Target: target, Ops: ops,
		Clients: tui.NewState([]clientconnect.Client(nil)), EndpointOpen: tui.NewState(false),
		Plan: tui.NewState(clientconnect.Plan{}), Error: tui.NewState(""),
		OnNotice: onNotice,
	}
}

func (d *Disclosure) endpointAction() string {
	if d.EndpointOpen.Get() {
		return "close ↵"
	}
	return "connect ↵"
}

func (d *Disclosure) toggleEndpoint() {
	opening := !d.EndpointOpen.Get()
	if opening {
		d.Clients.Set(d.Ops.Discover(d.Target))
	} else {
		d.closeChildren()
	}
	d.EndpointOpen.Set(opening)
}

func (d *Disclosure) closeChildren() {
	d.Plan.Set(clientconnect.Plan{})
	d.Error.Set("")
}

func (d *Disclosure) Back() bool {
	if d.Plan.Get().ClientID != "" {
		d.Plan.Set(clientconnect.Plan{})
		d.Error.Set("")
		return true
	}
	if d.EndpointOpen.Get() {
		d.closeChildren()
		d.EndpointOpen.Set(false)
		return true
	}
	return false
}

// KeyMap makes the entered disclosure the nearest Escape owner regardless of
// which descendant row currently holds selection.
func (d *Disclosure) KeyMap() tui.KeyMap {
	if !d.EndpointOpen.Get() {
		return nil
	}
	return tui.KeyMap{tui.OnPreemptStop(tui.KeyEscape, func(tui.KeyEvent) { d.Back() })}
}

func (d *Disclosure) clientAction(client clientconnect.Client) string {
	if client.Configured {
		return "configured"
	}
	if d.Plan.Get().ClientID == client.ID {
		return "close ↵"
	}
	return "configure ↵"
}

func (d *Disclosure) rowEscape(row *cockpitui.SelectableRow) *cockpitui.SelectableRow {
	row.OnEscape = func() { d.Back() }
	return row
}

func (d *Disclosure) chooseClient(client clientconnect.Client) {
	if client.Configured {
		return
	}
	if d.Plan.Get().ClientID == client.ID {
		d.Plan.Set(clientconnect.Plan{})
		d.Error.Set("")
		return
	}
	plan, err := d.Ops.Plan(client.ID, d.Target)
	if err != nil {
		d.Error.Set(err.Error())
		d.Plan.Set(clientconnect.Plan{})
		return
	}
	d.Error.Set("")
	d.Plan.Set(plan)
}

func (d *Disclosure) applyPlan() {
	plan := d.Plan.Get()
	if plan.ClientID == "" {
		return
	}
	if err := d.Ops.Apply(plan); err != nil {
		d.Error.Set(err.Error())
		return
	}
	clients := append([]clientconnect.Client(nil), d.Clients.Get()...)
	for i := range clients {
		if clients[i].ID == plan.ClientID {
			clients[i].Configured = true
		}
	}
	d.Clients.Set(clients)
	d.Plan.Set(clientconnect.Plan{})
	d.Error.Set("")
}

func (d *Disclosure) copyWorkspaceURL() {
	result := cockpitui.CopyToClipboard(d.Target.WorkspaceURL())
	d.publishNotice(copyNotice("Workspace", result))
}

func copyNotice(label string, result cockpitui.CopyResult) readmodel.Notice {
	switch result.Status {
	case cockpitui.CopyOK:
		return readmodel.Notice{Kind: readmodel.NoticeInfo, Message: label + " URL copied."}
	case cockpitui.CopySavedFile:
		return readmodel.Notice{Kind: readmodel.NoticeWarning, Message: label + " URL saved to " + result.Path + "."}
	default:
		return readmodel.Notice{Kind: readmodel.NoticeError, Message: label + " URL could not be copied or saved."}
	}
}

func (d *Disclosure) publishNotice(notice readmodel.Notice) {
	if d.OnNotice != nil {
		d.OnNotice(notice)
	}
}

func shortLocus(path string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

func displayChange(target clientconnect.Target, change clientconnect.Change) string {
	after := shortWorkspaceValue(target, change.After)
	if !change.BeforeExists {
		return "→ " + after
	}
	return shortWorkspaceValue(target, change.Before) + " → " + after
}

func shortWorkspaceValue(target clientconnect.Target, value string) string {
	workspace, workspaceErr := url.Parse(target.WorkspaceURL())
	parsed, err := url.Parse(value)
	if workspaceErr != nil || err != nil || !parsed.IsAbs() ||
		!strings.EqualFold(parsed.Scheme, workspace.Scheme) || !strings.EqualFold(parsed.Host, workspace.Host) {
		return value
	}
	return parsed.EscapedPath()
}

func (d *Disclosure) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*Disclosure)
	if !ok {
		return
	}
	d.Target = f.Target
	d.Ops = f.Ops
	d.OnNotice = f.OnNotice
}

var (
	_ tui.PropsUpdater = (*Disclosure)(nil)
	_ tui.KeyListener  = (*Disclosure)(nil)
)
