package ui

import (
	tui "github.com/grindlemire/go-tui"
)

// ConfirmActionPhase is the lifecycle of an inline confirm/destructive row.
//
// The row lives inline inside a section or feature capsule and owns the whole
// arm -> confirm -> submit -> fail loop locally, so a failed action stays on the
// row instead of surfacing a global banner. Consumers that do not need an async
// submit/fail loop set Async=false; those rows collapse to idle -> confirming
// and fire OnConfirm on the second activation.
type ConfirmActionPhase int

const (
	// ConfirmIdle is the resting row: one activation arms it.
	ConfirmIdle ConfirmActionPhase = iota
	// ConfirmConfirming is the armed row: activation runs the action.
	ConfirmConfirming
	// ConfirmSubmitting is shown only for async rows while OnConfirm runs.
	ConfirmSubmitting
	// ConfirmFailed keeps the error local and offers retry.
	ConfirmFailed
)

// ConfirmActionCopy is the caller-owned wording for each phase. Domain meaning
// (which object is deleted, the client-facing noun, the failure wording)
// stays with the consumer; only the mechanics live in the shared row.
type ConfirmActionCopy struct {
	Label string

	IdleValue  string
	IdleAction string

	ConfirmValue  string
	ConfirmAction string

	SubmittingValue string
	SubmittingHint  string

	FailedValue  string
	FailedAction string
}

// ConfirmActionRow is the shared inline confirm/destructive row primitive.
//
// It unifies the workspace-delete and route-delete flows (and any other inline
// two-step destructive confirmation) behind one interaction grammar: Enter to
// arm, Enter to confirm, Esc to cancel the confirmation, retry on failure. It
// does not own what gets acted on or where focus goes afterward; parents keep
// that via OnArm/OnCancel/OnConfirm.
type ConfirmActionRow struct {
	target *ActionTarget
	copy   ConfirmActionCopy

	phase   *tui.State[ConfirmActionPhase]
	errText *tui.State[string]

	// OnConfirm performs the action. It returns an error that is kept local and
	// shown in the failed phase.
	OnConfirm func() error
	// OnArm fires when the row transitions idle -> confirming.
	OnArm func()
	// OnCancel fires before returning to idle from a confirming/failed state.
	OnCancel func()
}

// NewConfirmActionRow builds an inline confirm row. The row arms on first
// activation, then runs OnConfirm on confirm; failures stay local and offer
// retry.
func NewConfirmActionRow(id string, copy ConfirmActionCopy, onConfirm func() error) *ConfirmActionRow {
	return &ConfirmActionRow{
		target:    NewActionTarget(id, nil),
		copy:      copy,
		phase:     tui.NewState(ConfirmIdle),
		errText:   tui.NewState(""),
		OnConfirm: onConfirm,
	}
}

// Phase exposes the current lifecycle phase for tests and render deps.
func (r *ConfirmActionRow) Phase() ConfirmActionPhase { return r.phase.Get() }

// IsOpen reports whether the row is armed or in an async submit/fail state.
func (r *ConfirmActionRow) IsOpen() bool { return r.phase.Get() != ConfirmIdle }

// OpenConfirm arms the confirmation.
func (r *ConfirmActionRow) OpenConfirm() {
	r.errText.Set("")
	r.phase.Set(ConfirmConfirming)
	if r.OnArm != nil {
		r.OnArm()
	}
}

// Cancel returns to idle without acting.
func (r *ConfirmActionRow) Cancel() {
	if r.OnCancel != nil {
		r.OnCancel()
	}
	r.errText.Set("")
	r.phase.Set(ConfirmIdle)
}

// Confirm runs the action. Failures stay local and switch the row to the failed
// phase with retry.
func (r *ConfirmActionRow) Confirm() {
	if r.OnConfirm == nil {
		return
	}
	r.errText.Set("")
	r.phase.Set(ConfirmSubmitting)
	if err := r.OnConfirm(); err != nil {
		r.errText.Set(err.Error())
		r.phase.Set(ConfirmFailed)
		return
	}
}

// SetCopy replaces the row wording; used by consumers that recompute copy per
// render (for example, when the acted-on object's name changes).
func (r *ConfirmActionRow) SetCopy(copy ConfirmActionCopy) { r.copy = copy }

func (r *ConfirmActionRow) BindApp(app *tui.App) {
	r.target.BindApp(app)
	r.phase.BindApp(app)
	r.errText.BindApp(app)
}

func (r *ConfirmActionRow) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*ConfirmActionRow)
	if !ok {
		return
	}
	r.copy = f.copy
	r.OnConfirm = f.OnConfirm
	r.OnArm = f.OnArm
	r.OnCancel = f.OnCancel
}

func (r *ConfirmActionRow) Render(_ *tui.App) *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column),
		tui.WithWidthPercent(100),
	)

	var row *tui.Element
	switch r.phase.Get() {
	case ConfirmConfirming:
		opts := append(r.target.ShellOptions(), tui.WithOnActivate(r.Confirm))
		row = ActionRow(r.target.Marker(), r.copy.Label, r.copy.ConfirmValue, r.copy.ConfirmAction, opts...)
	case ConfirmSubmitting:
		row = ActionRow(r.target.Marker(), r.copy.Label, r.copy.SubmittingValue, r.copy.SubmittingHint, r.target.ShellOptions()...)
	case ConfirmFailed:
		opts := append(r.target.ShellOptions(), tui.WithOnActivate(r.Confirm))
		row = ActionRow(r.target.Marker(), r.copy.Label, r.copy.FailedValue, r.copy.FailedAction, opts...)
	default:
		opts := append(r.target.ShellOptions(), tui.WithOnActivate(r.OpenConfirm))
		row = ActionRow(r.target.Marker(), r.copy.Label, r.copy.IdleValue, r.copy.IdleAction, opts...)
	}
	r.target.BindElement(row)
	root.AddChild(row)

	if sub := r.subText(); sub != "" {
		detail := tui.New(
			tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
			tui.WithWidthPercent(100),
		)
		detail.AddChild(tui.New(tui.WithWidth(2)))
		detail.AddChild(tui.New(tui.WithText("  " + sub)))
		root.AddChild(detail)
	}
	return root
}

// subText is the indented second line for local failure text.
func (r *ConfirmActionRow) subText() string {
	if r.phase.Get() == ConfirmFailed {
		return r.errText.Get()
	}
	return ""
}

func (r *ConfirmActionRow) KeyMap() tui.KeyMap {
	switch r.phase.Get() {
	case ConfirmConfirming, ConfirmFailed:
		return r.target.KeyMap(r.Confirm, r.Cancel)
	case ConfirmSubmitting:
		// Escape while submitting is a no-op; the row owns no local dismissal.
		return r.target.KeyMap(nil, nil)
	default:
		return r.target.KeyMap(r.OpenConfirm, nil)
	}
}

func (r *ConfirmActionRow) IsFocused() bool { return r.target.IsFocused() }

var (
	_ tui.Component    = (*ConfirmActionRow)(nil)
	_ tui.KeyListener  = (*ConfirmActionRow)(nil)
	_ tui.PropsUpdater = (*ConfirmActionRow)(nil)
	_ tui.AppBinder    = (*ConfirmActionRow)(nil)
)
