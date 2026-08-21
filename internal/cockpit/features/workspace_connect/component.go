package workspace_connect

import (
	"os"
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/clientconnect"
	cockpitui "github.com/swobuforge/swobu/internal/cockpit/ui"
)

type connectOperations interface {
	Discover(target clientconnect.Target) []clientconnect.Client
	Plan(clientID clientconnect.ClientID, target clientconnect.Target) (clientconnect.Plan, error)
	Apply(plan clientconnect.Plan) error
}

type childKind int

const (
	childNone childKind = iota
	childPlan
	childManual
)

type childScope struct {
	kind childKind
	plan clientconnect.Plan
}

func (c childScope) isManual() bool {
	return c.kind == childManual
}

func (c childScope) hasPlan(id clientconnect.ClientID) bool {
	return c.kind == childPlan && c.plan.ClientID == id
}

type copyFeedback struct {
	key    string
	result cockpitui.CopyResult
}

type Disclosure struct {
	Target       clientconnect.Target
	Ops          connectOperations
	Clients      *tui.State[[]clientconnect.Client]
	EndpointOpen *tui.State[bool]
	Child        *tui.State[childScope]
	Feedback     *tui.State[copyFeedback]
	Error        *tui.State[string]
}

func New(target clientconnect.Target, ops connectOperations) *Disclosure {
	if ops == nil {
		ops = clientconnect.NewService()
	}
	return &Disclosure{
		Target:       target,
		Ops:          ops,
		Clients:      tui.NewState([]clientconnect.Client(nil)),
		EndpointOpen: tui.NewState(false),
		Child:        tui.NewState(childScope{}),
		Feedback:     tui.NewState(copyFeedback{}),
		Error:        tui.NewState(""),
	}
}

func (d *Disclosure) BindApp(app *tui.App) {
	if d.Clients != nil {
		d.Clients.BindApp(app)
	}
	if d.EndpointOpen != nil {
		d.EndpointOpen.BindApp(app)
	}
	if d.Child != nil {
		d.Child.BindApp(app)
	}
	if d.Feedback != nil {
		d.Feedback.BindApp(app)
	}
	if d.Error != nil {
		d.Error.BindApp(app)
	}
}

func (d *Disclosure) UnbindApp() {}

func (d *Disclosure) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*Disclosure)
	if !ok {
		return
	}
	d.Target = f.Target
	d.Ops = f.Ops
}

func (d *Disclosure) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnPreemptStop(tui.KeyEscape, func(tui.KeyEvent) {
			if d.EndpointOpen.Get() {
				d.Back()
			}
		}),
	}
}

func (d *Disclosure) Back() bool {
	if d.Child.Get().kind != childNone {
		d.closeChildScope()
		return true
	}
	if d.EndpointOpen.Get() {
		d.closeChildren()
		d.EndpointOpen.Set(false)
		return true
	}
	return false
}

func (d *Disclosure) rowEscape(row *cockpitui.SelectableRow) *cockpitui.SelectableRow {
	row.OnEscape = func() {
		d.Back()
	}
	row.UpdateProps(row)
	return row
}

func (d *Disclosure) endpointAction() string {
	if d.EndpointOpen.Get() {
		return "close \u21b5"
	}
	return "clients \u21b5"
}

func (d *Disclosure) clientAction(client clientconnect.Client) string {
	if client.Configured {
		return "configured"
	}
	return "configure \u21b5"
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

func (d *Disclosure) closeChildScope() {
	d.Child.Set(childScope{})
	d.Feedback.Set(copyFeedback{})
	d.Error.Set("")
}

func (d *Disclosure) closeChildren() {
	d.closeChildScope()
}

func (d *Disclosure) chooseClient(client clientconnect.Client) {
	if client.Configured {
		return
	}
	plan, err := d.Ops.Plan(client.ID, d.Target)
	if err != nil {
		d.Error.Set(err.Error())
		d.Child.Set(childScope{})
		return
	}
	d.Error.Set("")
	d.Feedback.Set(copyFeedback{})
	d.Child.Set(childScope{kind: childPlan, plan: plan})
}

func (d *Disclosure) openManualSetup() {
	d.Feedback.Set(copyFeedback{})
	d.Error.Set("")
	d.Child.Set(childScope{kind: childManual})
}

func (d *Disclosure) applyPlan() {
	child := d.Child.Get()
	if child.kind != childPlan || child.plan.ClientID == "" {
		return
	}
	if err := d.Ops.Apply(child.plan); err != nil {
		d.Error.Set(err.Error())
		return
	}
	d.Clients.Set(d.Ops.Discover(d.Target))
	d.closeChildScope()
}

func (d *Disclosure) copyItem(key, value string) {
	result := cockpitui.CopyToClipboard(value)
	d.Feedback.Set(copyFeedback{key: key, result: result})
	d.Error.Set("")
	if result.Status == cockpitui.CopyFailed {
		d.Error.Set("copy failed \u00b7 run swobu doctor --copy")
	}
}

func (d *Disclosure) copyAction(key string) string {
	fb := d.Feedback.Get()
	if fb.key != key {
		return "copy \u21b5"
	}
	switch fb.result.Status {
	case cockpitui.CopyOK:
		return "copied"
	case cockpitui.CopySavedFile:
		return "saved"
	default:
		return "copy failed"
	}
}

func shortLocus(path string) string {
	home, err := os.UserHomeDir()
	if err == nil {
		if path == home {
			return "~"
		}
		prefix := home + string(os.PathSeparator)
		if strings.HasPrefix(path, prefix) {
			return "~" + string(os.PathSeparator) + strings.TrimPrefix(path, prefix)
		}
	}
	return path
}

func displayChange(target clientconnect.Target, change clientconnect.Change) string {
	after := shortWorkspaceValue(target, change.After)
	if !change.BeforeExists {
		return "\u2192 " + after
	}
	return shortWorkspaceValue(target, change.Before) + " \u2192 " + after
}

func shortWorkspaceValue(target clientconnect.Target, raw string) string {
	if target.WorkspaceURL() == "" {
		return raw
	}
	prefix := target.WorkspaceURL()
	if strings.HasPrefix(raw, prefix) {
		suffix := strings.TrimPrefix(raw, prefix)
		return "/c/" + target.WorkspaceSlug() + suffix
	}
	return raw
}

var (
	_ tui.Component    = (*Disclosure)(nil)
	_ tui.PropsUpdater = (*Disclosure)(nil)
	_ tui.KeyListener  = (*Disclosure)(nil)
	_ tui.AppBinder    = (*Disclosure)(nil)
	_ tui.AppUnbinder  = (*Disclosure)(nil)
)
