package target_config

import (
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

// AmbientOrReferenceAuthenticationProps is the complete reusable component
// contract. Labels come from profile metadata and mutations leave through
// events; the component receives neither provider identity nor feature state.
type AmbientOrReferenceAuthenticationProps struct {
	ID              string
	AmbientLabel    string
	ReferenceLabel  string
	SuggestedEnvVar string
	Ref             string
	Apply           func(string)
	Store           func(string) (string, error)
}

type ambientOrReferenceStage uint8

const (
	ambientOrReferenceClosed ambientOrReferenceStage = iota
	ambientOrReferenceMenu
	ambientOrReferenceChooser
)

// ambientOrReferenceAuthentication owns only its closed interaction stage and
// the nested credential chooser. The durable reference remains parent-owned.
type ambientOrReferenceAuthentication struct {
	props   AmbientOrReferenceAuthenticationProps
	stage   *tui.State[ambientOrReferenceStage]
	ref     *tui.State[string]
	chooser *tui.State[*credentialRow]
}

func AmbientOrReferenceAuthentication(props AmbientOrReferenceAuthenticationProps) tui.Component {
	a := &ambientOrReferenceAuthentication{
		props: props,
		stage: tui.NewState(ambientOrReferenceClosed),
		ref:   tui.NewState(strings.TrimSpace(props.Ref)),
	}
	chooser := newCredentialField(CredentialFieldProps{
		ID:              props.ID + ":reference",
		SuggestedEnvVar: props.SuggestedEnvVar,
		Store:           props.Store,
		ChoiceAction:    "select ↵",
	})
	chooser.props.Apply = func(ref string) {
		a.apply(ref)
		a.stage.Set(ambientOrReferenceClosed)
	}
	chooser.stage.Set(credStageMenu)
	a.chooser = tui.NewState(chooser)
	return a
}

func (a *ambientOrReferenceAuthentication) BindApp(app *tui.App) {
	a.bindAppFields(app)
	a.chooser.Get().BindApp(app)
}
func (a *ambientOrReferenceAuthentication) UnbindApp() {}

func (a *ambientOrReferenceAuthentication) KeyMap() tui.KeyMap {
	return ui.BackScope(func() bool { return a.stage.Get() != ambientOrReferenceClosed }, a.retreat)
}

func (a *ambientOrReferenceAuthentication) key(suffix string) string {
	return a.props.ID + ":" + suffix
}

func (a *ambientOrReferenceAuthentication) toggle() {
	if a.stage.Get() == ambientOrReferenceClosed {
		a.stage.Set(ambientOrReferenceMenu)
		return
	}
	a.stage.Set(ambientOrReferenceClosed)
}

func (a *ambientOrReferenceAuthentication) open() {
	if a.stage.Get() == ambientOrReferenceClosed {
		a.stage.Set(ambientOrReferenceMenu)
	}
}

func (a *ambientOrReferenceAuthentication) retreat() {
	if a.stage.Get() == ambientOrReferenceChooser {
		if a.chooser.Get().stage.Get() != credStageMenu {
			a.chooser.Get().stage.Set(credStageMenu)
			return
		}
		a.stage.Set(ambientOrReferenceMenu)
		return
	}
	a.stage.Set(ambientOrReferenceClosed)
}

func (a *ambientOrReferenceAuthentication) apply(ref string) {
	ref = strings.TrimSpace(ref) // swobu:io-string source=boundary
	a.ref.Set(ref)
	if a.props.Apply != nil {
		a.props.Apply(ref)
	}
}

func (a *ambientOrReferenceAuthentication) useAmbient() {
	a.apply("")
	a.stage.Set(ambientOrReferenceClosed)
}

func (a *ambientOrReferenceAuthentication) chooseReference() {
	a.chooser.Get().reset()
	a.chooser.Get().stage.Set(credStageMenu)
	a.stage.Set(ambientOrReferenceChooser)
}

func (a *ambientOrReferenceAuthentication) referenceSource() string {
	return credentialSourceDisplay(credentialref.Parse(a.ref.Get()))
}

func (a *ambientOrReferenceAuthentication) referenceDetail() string {
	parsed := credentialref.Parse(a.ref.Get())
	_, detail, ok := strings.Cut(parsed.String(), ":")
	if !ok {
		return parsed.String()
	}
	return detail
}

func AmbientOrReferenceHeader(a *ambientOrReferenceAuthentication) *ui.SelectableRow {
	value := a.props.AmbientLabel
	if strings.TrimSpace(a.ref.Get()) != "" {
		value = a.props.ReferenceLabel + " · " + a.referenceSource()
	} else if a.stage.Get() == ambientOrReferenceChooser {
		value = a.props.ReferenceLabel
	}
	action := "manage ↵"
	if a.stage.Get() != ambientOrReferenceClosed {
		action = "close ↵"
	}
	return ui.NewSelectableRow(a.key("header"), "authentication", value, action, a.toggle)
}

func AmbientOrReferenceUseReferenceOption(a *ambientOrReferenceAuthentication) *ui.SelectableRow {
	return ui.NewSelectableRow(a.key("use-reference"), "", "use "+a.props.ReferenceLabel, "select ↵", a.chooseReference)
}

func AmbientOrReferenceChangeOption(a *ambientOrReferenceAuthentication) *ui.SelectableRow {
	return ui.NewSelectableRow(a.key("change-reference"), "", "change credential", "enter ↵", a.chooseReference)
}

func AmbientOrReferenceUseAmbientOption(a *ambientOrReferenceAuthentication) *ui.SelectableRow {
	return ui.NewSelectableRow(a.key("use-ambient"), "", "use "+a.props.AmbientLabel, "select ↵", a.useAmbient)
}

templ (a *ambientOrReferenceAuthentication) Render() {
	<div class="flex-col w-full" deps={a.stage, a.ref}>
		@AmbientOrReferenceHeader(a)
		if a.stage.Get() == ambientOrReferenceMenu {
			<div class="pl-3 flex-col w-full">
				if strings.TrimSpace(a.ref.Get()) == "" {
					@AmbientOrReferenceUseReferenceOption(a)
				} else {
					<div class="w-full">@FlowText(a.referenceDetail())</div>
					@AmbientOrReferenceChangeOption(a)
					@AmbientOrReferenceUseAmbientOption(a)
				}
			</div>
		} else if a.stage.Get() == ambientOrReferenceChooser {
			<div class="pl-3 flex-col w-full">@CredentialChooser(a.chooser.Get())</div>
		} else if detail := a.referenceDetail(); detail != "" {
			<div class="pl-18 w-full">@FlowText(detail)</div>
		}
	</div>
}
