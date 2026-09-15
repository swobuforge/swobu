package workspace_connect

import (
	"context"
	"os"
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/clientconnect"
	cockpitui "github.com/swobuforge/swobu/internal/cockpit/ui"
)

type connectOperations interface {
	Discover(context.Context, clientconnect.Target) []clientconnect.Client
	Plan(context.Context, clientconnect.ClientID, clientconnect.Target) (clientconnect.Plan, error)
	Apply(context.Context, clientconnect.Plan) (clientconnect.Plan, error)
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
	endpointContext    context.Context
	endpointCancel     context.CancelFunc
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
	d.cancelEndpoint()
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
	if !d.EndpointOpen.Get() {
		return nil
	}
	return tui.KeyMap{tui.OnPreemptStop(tui.KeyEscape, func(tui.KeyEvent) {
		d.Back()
	})}
}

func (d *Disclosure) Back() bool {
	if d.Child.Get().kind != childNone {
		d.closeChildScope()
		return true
	}
	if d.EndpointOpen.Get() {
		d.cancelEndpoint()
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
		d.cancelEndpoint()
		d.endpointContext, d.endpointCancel = context.WithCancel(context.Background())
		d.endpointGeneration++
		d.EndpointOpen.Set(true)
		d.DiscoveryPending.Set(true)
		d.Observations.Set(nil)
		d.startDiscovery(d.endpointGeneration)
	} else {
		d.cancelEndpoint()
		d.endpointGeneration++
		d.closeChildren()
		d.DiscoveryPending.Set(false)
		d.EndpointOpen.Set(false)
	}
}

func (d *Disclosure) cancelEndpoint() {
	if d.endpointCancel != nil {
		d.endpointCancel()
	}
	d.endpointContext = nil
	d.endpointCancel = nil
}

func (d *Disclosure) operationContext() context.Context {
	if d.endpointContext != nil {
		return d.endpointContext
	}
	return context.Background()
}

func (d *Disclosure) startDiscovery(endpointGen uint64) {
	target := d.Target
	ops := d.Ops
	ctx := d.operationContext()
	if !d.hasLiveApp() {
		clients := ops.Discover(ctx, target)
		obsList := make([]clientObservation, len(clients))
		for i, c := range clients {
			obsList[i] = clientObservation{
				Client: c,
				Kind:   observationChecking,
			}
		}
		for i, c := range clients {
			plan, err := ops.Plan(ctx, c.ID, target)
			if err != nil {
				obsList[i].Kind = observationFailed
				obsList[i].Err = inspectionError(err)
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
		clients := ops.Discover(ctx, target)
		app.QueueUpdate(func() {
			if d.endpointGeneration != endpointGen || !d.EndpointOpen.Get() {
				return
			}
			d.DiscoveryPending.Set(false)
			obsList := make([]clientObservation, len(clients))
			for i, c := range clients {
				obsList[i] = clientObservation{
					Client: c,
					Kind:   observationChecking,
				}
			}
			d.Observations.Set(obsList)

			// Launch parallel Plan inspections for each discovered client
			for _, c := range clients {
				d.launchInspection(ctx, endpointGen, c.ID, target, ops, app)
			}
		})
	}()
}

func (d *Disclosure) launchInspection(ctx context.Context, endpointGen uint64, clientID clientconnect.ClientID, target clientconnect.Target, ops connectOperations, app *tui.App) {
	if app == nil {
		return
	}
	go func() {
		plan, err := ops.Plan(ctx, clientID, target)
		app.QueueUpdate(func() {
			if d.endpointGeneration != endpointGen || !d.EndpointOpen.Get() {
				return
			}
			obsList := append([]clientObservation(nil), d.Observations.Get()...)
			for i := range obsList {
				if obsList[i].Client.ID == clientID {
					if err != nil {
						obsList[i].Kind = observationFailed
						obsList[i].Err = inspectionError(err)
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

func (d *Disclosure) observationForClient(clientID clientconnect.ClientID) (clientObservation, bool) {
	for _, obs := range d.Observations.Get() {
		if obs.Client.ID == clientID {
			return obs, true
		}
	}
	return clientObservation{}, false
}

func (d *Disclosure) chooseClient(clientID clientconnect.ClientID) {
	if d.Child.Get().isClient(clientID) {
		d.closeChildScope()
		return
	}

	// Look up current observation state. If the client is not present,
	// do not open child scope or trigger an inspection.
	currentObs, ok := d.observationForClient(clientID)
	if !ok {
		return
	}

	d.Child.Set(childScope{kind: childClient, clientID: clientID})
	d.Feedback.Set(copyFeedback{})

	if currentObs.Kind == observationChecking || currentObs.Applying {
		return
	}

	// Trigger fresh inspection on activation (configured, needs change, or failed)
	endpointGen := d.endpointGeneration
	target := d.Target
	ops := d.Ops
	ctx := d.operationContext()

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
		plan, err := ops.Plan(ctx, clientID, target)
		obsList = append([]clientObservation(nil), d.Observations.Get()...)
		for i := range obsList {
			if obsList[i].Client.ID == clientID {
				if err != nil {
					obsList[i].Kind = observationFailed
					obsList[i].Err = inspectionError(err)
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

	d.launchInspection(ctx, endpointGen, clientID, target, ops, d.app)
}

func inspectionError(err error) string {
	return err.Error() + "\nNothing changed."
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
	ops := d.Ops
	ctx := d.operationContext()

	nextObsList := append([]clientObservation(nil), obsList...)
	nextObsList[targetIdx].Applying = true
	nextObsList[targetIdx].Err = ""
	d.Observations.Set(nextObsList)

	if !d.hasLiveApp() {
		verified, err := ops.Apply(ctx, plan)
		updated := append([]clientObservation(nil), d.Observations.Get()...)
		for i := range updated {
			if updated[i].Client.ID == clientID {
				storeApplyResult(&updated[i], verified, err)
				break
			}
		}
		d.Observations.Set(updated)
		if err == nil && verified.AlreadyConfigured() && d.Child.Get().isClient(clientID) {
			d.closeChildScope()
		}
		return
	}

	app := d.app
	go func() {
		verified, err := ops.Apply(ctx, plan)
		app.QueueUpdate(func() {
			if d.endpointGeneration != endpointGen || !d.EndpointOpen.Get() {
				return
			}
			updated := append([]clientObservation(nil), d.Observations.Get()...)
			for i := range updated {
				if updated[i].Client.ID == clientID {
					storeApplyResult(&updated[i], verified, err)
					break
				}
			}
			d.Observations.Set(updated)
			if err == nil && verified.AlreadyConfigured() && d.Child.Get().isClient(clientID) {
				d.closeChildScope()
			}
		})
	}()
}

func storeApplyResult(observation *clientObservation, verified clientconnect.Plan, err error) {
	observation.Applying = false
	if len(verified.ConfigPaths) == 0 && verified.ClientID == "" {
		observation.Kind = observationFailed
		observation.Plan = clientconnect.Plan{}
	} else {
		observation.Plan = verified
		if verified.AlreadyConfigured() {
			observation.Kind = observationMatch
		} else {
			observation.Kind = observationNeedsChange
		}
	}
	if err != nil {
		observation.Err = err.Error()
	} else {
		observation.Err = ""
	}
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

func shortLoci(paths []string) string {
	short := make([]string, len(paths))
	for i, path := range paths {
		short[i] = shortLocus(path)
	}
	return strings.Join(short, ", ")
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
