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

type observationKind uint8

const (
	observationChecking observationKind = iota + 1
	observationMatch
	observationNeedsChange
	observationFailed
)

type clientObservation struct {
	Client   clientconnect.Client
	Kind     observationKind
	Plan     clientconnect.Plan
	Err      string
	Applying bool
}

type childKind int

const (
	childNone childKind = iota
	childClient
	childManual
)

type childScope struct {
	kind     childKind
	clientID clientconnect.ClientID
}

func (c childScope) isManual() bool {
	return c.kind == childManual
}

func (c childScope) isClient(id clientconnect.ClientID) bool {
	return c.kind == childClient && c.clientID == id
}

type copyFeedback struct {
	key    string
	result cockpitui.CopyResult
}

type Disclosure struct {
	Target             clientconnect.Target
	Ops                connectOperations
	DiscoveryPending   *tui.State[bool]
	Observations       *tui.State[[]clientObservation]
	EndpointOpen       *tui.State[bool]
	Child              *tui.State[childScope]
	Feedback           *tui.State[copyFeedback]
	app                *tui.App
	endpointGeneration uint64
	clientSeq          map[clientconnect.ClientID]uint64
}

func New(target clientconnect.Target, ops connectOperations) *Disclosure {
	if ops == nil {
		ops = clientconnect.NewService()
	}
	return &Disclosure{
		Target:           target,
		Ops:              ops,
		DiscoveryPending: tui.NewState(false),
		Observations:     tui.NewState([]clientObservation(nil)),
		EndpointOpen:     tui.NewState(false),
		Child:            tui.NewState(childScope{}),
		Feedback:         tui.NewState(copyFeedback{}),
		clientSeq:        make(map[clientconnect.ClientID]uint64),
	}
}

func (d *Disclosure) BindApp(app *tui.App) {
	d.app = app
	if d.DiscoveryPending != nil {
		d.DiscoveryPending.BindApp(app)
	}
	if d.Observations != nil {
		d.Observations.BindApp(app)
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
}

func (d *Disclosure) UnbindApp() {
	d.endpointGeneration++
	d.app = nil
}

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
		d.endpointGeneration++
		d.closeChildren()
		d.DiscoveryPending.Set(false)
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
		return "close ↵"
	}
	return "clients ↵"
}

func (d *Disclosure) hasLiveApp() bool {
	if d.app == nil {
		return false
	}
	select {
	case <-d.app.StopCh():
		return false
	default:
		return true
	}
}

func (d *Disclosure) toggleEndpoint() {
	opening := !d.EndpointOpen.Get()
	if opening {
		d.endpointGeneration++
		d.EndpointOpen.Set(true)
		d.DiscoveryPending.Set(true)
		d.Observations.Set(nil)
		d.startDiscovery(d.endpointGeneration)
	} else {
		d.endpointGeneration++
		d.closeChildren()
		d.DiscoveryPending.Set(false)
		d.EndpointOpen.Set(false)
	}
}

func (d *Disclosure) startDiscovery(endpointGen uint64) {
	target := d.Target
	if !d.hasLiveApp() {
		clients := d.Ops.Discover(target)
		obsList := make([]clientObservation, len(clients))
		for i, c := range clients {
			obsList[i] = clientObservation{
				Client: c,
				Kind:   observationChecking,
			}
		}
		for i, c := range clients {
			plan, err := d.Ops.Plan(c.ID, target)
			if err != nil {
				obsList[i].Kind = observationFailed
				obsList[i].Err = err.Error()
			} else if plan.AlreadyConfigured() {
				obsList[i].Kind = observationMatch
				obsList[i].Plan = plan
			} else {
				obsList[i].Kind = observationNeedsChange
				obsList[i].Plan = plan
			}
		}
		d.DiscoveryPending.Set(false)
		d.Observations.Set(obsList)
		return
	}

	app := d.app
	go func() {
		clients := d.Ops.Discover(target)
		app.QueueUpdate(func() {
			if d.endpointGeneration != endpointGen || !d.EndpointOpen.Get() {
				return
			}
			d.DiscoveryPending.Set(false)
			obsList := make([]clientObservation, len(clients))
			for i, c := range clients {
				d.clientSeq[c.ID]++
				obsList[i] = clientObservation{
					Client: c,
					Kind:   observationChecking,
				}
			}
			d.Observations.Set(obsList)

			// Launch parallel Plan inspections for each discovered client
			for _, c := range clients {
				d.launchInspection(endpointGen, c.ID, d.clientSeq[c.ID])
			}
		})
	}()
}

func (d *Disclosure) launchInspection(endpointGen uint64, clientID clientconnect.ClientID, clientSeq uint64) {
	target := d.Target
	app := d.app
	if app == nil {
		return
	}
	go func() {
		plan, err := d.Ops.Plan(clientID, target)
		app.QueueUpdate(func() {
			if d.endpointGeneration != endpointGen || !d.EndpointOpen.Get() {
				return
			}
			if d.clientSeq[clientID] != clientSeq {
				return
			}
			obsList := append([]clientObservation(nil), d.Observations.Get()...)
			for i := range obsList {
				if obsList[i].Client.ID == clientID {
					if err != nil {
						obsList[i].Kind = observationFailed
						obsList[i].Err = err.Error()
						obsList[i].Plan = clientconnect.Plan{}
					} else if plan.AlreadyConfigured() {
						obsList[i].Kind = observationMatch
						obsList[i].Plan = plan
						obsList[i].Err = ""
					} else {
						obsList[i].Kind = observationNeedsChange
						obsList[i].Plan = plan
						obsList[i].Err = ""
					}
					break
				}
			}
			d.Observations.Set(obsList)
		})
	}()
}

func (d *Disclosure) closeChildScope() {
	d.Child.Set(childScope{})
	d.Feedback.Set(copyFeedback{})
}

func (d *Disclosure) closeChildren() {
	d.closeChildScope()
}

func (d *Disclosure) chooseClient(obs clientObservation) {
	clientID := obs.Client.ID
	if d.Child.Get().isClient(clientID) {
		d.closeChildScope()
		return
	}
	d.Child.Set(childScope{kind: childClient, clientID: clientID})
	d.Feedback.Set(copyFeedback{})

	// If already in-flight checking or applying, do not launch a new Plan
	if obs.Kind == observationChecking || obs.Applying {
		return
	}

	// Trigger fresh inspection on activation (configured, needs change, or failed)
	d.clientSeq[clientID]++
	clientSeq := d.clientSeq[clientID]
	endpointGen := d.endpointGeneration

	obsList := append([]clientObservation(nil), d.Observations.Get()...)
	for i := range obsList {
		if obsList[i].Client.ID == clientID {
			obsList[i].Kind = observationChecking
			obsList[i].Err = ""
			break
		}
	}
	d.Observations.Set(obsList)

	if !d.hasLiveApp() {
		plan, err := d.Ops.Plan(clientID, d.Target)
		obsList = append([]clientObservation(nil), d.Observations.Get()...)
		for i := range obsList {
			if obsList[i].Client.ID == clientID {
				if err != nil {
					obsList[i].Kind = observationFailed
					obsList[i].Err = err.Error()
					obsList[i].Plan = clientconnect.Plan{}
				} else if plan.AlreadyConfigured() {
					obsList[i].Kind = observationMatch
					obsList[i].Plan = plan
				} else {
					obsList[i].Kind = observationNeedsChange
					obsList[i].Plan = plan
				}
				break
			}
		}
		d.Observations.Set(obsList)
		return
	}

	d.launchInspection(endpointGen, clientID, clientSeq)
}

func (d *Disclosure) openManualSetup() {
	d.Feedback.Set(copyFeedback{})
	d.Child.Set(childScope{kind: childManual})
}

func (d *Disclosure) applyPlan(clientID clientconnect.ClientID) {
	obsList := d.Observations.Get()
	var targetObs *clientObservation
	targetIdx := -1
	for i := range obsList {
		if obsList[i].Client.ID == clientID {
			targetObs = &obsList[i]
			targetIdx = i
			break
		}
	}
	if targetObs == nil || targetObs.Applying || targetObs.Kind != observationNeedsChange {
		return
	}

	plan := targetObs.Plan
	endpointGen := d.endpointGeneration

	nextObsList := append([]clientObservation(nil), obsList...)
	nextObsList[targetIdx].Applying = true
	nextObsList[targetIdx].Err = ""
	d.Observations.Set(nextObsList)

	if !d.hasLiveApp() {
		err := d.Ops.Apply(plan)
		updated := append([]clientObservation(nil), d.Observations.Get()...)
		for i := range updated {
			if updated[i].Client.ID == clientID {
				updated[i].Applying = false
				if err != nil {
					updated[i].Err = err.Error()
				} else {
					updated[i].Kind = observationMatch
					updated[i].Plan = clientconnect.Plan{
						ClientID:   plan.ClientID,
						ClientName: plan.ClientName,
						ConfigPath: plan.ConfigPath,
						Target:     plan.Target,
						Changes:    nil,
					}
					updated[i].Err = ""
				}
				break
			}
		}
		d.Observations.Set(updated)
		if err == nil && d.Child.Get().isClient(clientID) {
			d.closeChildScope()
		}
		return
	}

	app := d.app
	go func() {
		err := d.Ops.Apply(plan)
		app.QueueUpdate(func() {
			if d.endpointGeneration != endpointGen || !d.EndpointOpen.Get() {
				return
			}
			updated := append([]clientObservation(nil), d.Observations.Get()...)
			for i := range updated {
				if updated[i].Client.ID == clientID {
					updated[i].Applying = false
					if err != nil {
						updated[i].Err = err.Error()
					} else {
						updated[i].Kind = observationMatch
						updated[i].Plan = clientconnect.Plan{
							ClientID:   plan.ClientID,
							ClientName: plan.ClientName,
							ConfigPath: plan.ConfigPath,
							Target:     plan.Target,
							Changes:    nil,
						}
						updated[i].Err = ""
					}
					break
				}
			}
			d.Observations.Set(updated)
			if err == nil && d.Child.Get().isClient(clientID) {
				d.closeChildScope()
			}
		})
	}()
}

func (d *Disclosure) copyItem(key, value string) {
	result := cockpitui.CopyToClipboard(value)
	d.Feedback.Set(copyFeedback{key: key, result: result})
}

func (d *Disclosure) copyAction(key string) string {
	fb := d.Feedback.Get()
	if fb.key != key {
		return "copy ↵"
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
		return "→ " + after
	}
	return shortWorkspaceValue(target, change.Before) + " → " + after
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
