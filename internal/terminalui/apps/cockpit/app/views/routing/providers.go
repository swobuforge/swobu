// Provider panel views for routing section.
package routing

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// BuildProvidersCreatePanel shows provider setup in create mode.
func BuildProvidersCreatePanel(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	configured := 0
	draftProvider := selectors.CreateDraftProviderConfig(model)
	if draftProvider != nil {
		configured = 1
	}
	open, setOpen := retained.UseState(ctx, func() bool { return false })
	expanded, setExpanded := retained.UseState(ctx, func() bool { return false })
	picker, setPicker := retained.UseState(ctx, func() views.FilterablePickerState { return views.DefaultFilterablePickerState() })
	var cancelFn func() []update.Action
	if open || expanded {
		cancelFn = func() []update.Action {
			setOpen(false)
			setExpanded(false)
			return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeNAV}}
		}
	}
	parent := views.RowManageWithHooks(views.RowProviders, fmt.Sprintf("%d configured", configured), func() []update.Action {
		if open {
			setOpen(false)
			setExpanded(false)
			return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeNAV}}
		}
		setOpen(true)
		views.ResetFilterablePickerState(setPicker)
		return []update.Action{
			state.SetInteractionMode{Mode: state.InteractionModeManageList},
			interaction.FocusKeyAction{Key: views.FilterablePickerFocusKey("providers-create-option", 0)},
		}
	}, cancelFn, views.FocusAffordance("manage", false))
	var out retained.ViewSpec[state.Model]
	if !open {
		out = parent
	} else {
		onClose := func() []update.Action {
			setOpen(false)
			setExpanded(false)
			return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeNAV}}
		}
		if draftProvider == nil {
			out = views.RenderFilterablePickerDisclosure(ctx, parent, picker, setPicker, createProviderSpecItems(model, onClose), views.FilterablePickerConfig{
				KeyPrefix:      "providers-create-option",
				BuildOptionRow: views.ChoicePickerOptionRow(true),
				WindowSize:     6,
				FindLabel:      "find",
				ShowSelected:   true,
				OnNoMatchFocus: func() []update.Action { return []update.Action{interaction.FocusKeyAction{Key: "providers"}} },
				OnCancel: func() []update.Action {
					setOpen(false)
					return []update.Action{
						state.SetInteractionMode{Mode: state.InteractionModeNAV},
						interaction.FocusKeyAction{Key: "providers"},
					}
				},
			})
		} else {
			providerRow := newProviderSummaryRow(
				*draftProvider,
				true,
				expanded,
				func() []update.Action {
					setExpanded(!expanded)
					return nil
				},
				onClose,
			)
			rows := []retained.ViewSpec[state.Model]{providerRow}
			if expanded {
				rows = append(rows, createProviderPropertyRows("", draftProvider, true, model)...)
			}
			out = toolkitviews.NewAnchoredDisclosure(parent, rows...)
		}
	}
	return out
}

// BuildProvidersWorkspacePanel shows provider setup in workspace mode.
func BuildProvidersWorkspacePanel(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	snapshot := selectors.CurrentEndpointSnapshot(model)
	if snapshot == nil {
		return views.RowStatic("", "not configured")
	}
	return buildProvidersWorkspaceConfiguredPanel(ctx, model, snapshot)
}

func createProviderSpecItems(model state.Model, onCancel func() []update.Action) []views.FilterablePickerItem {
	options := state.ProviderOptions()
	if len(options) == 0 {
		return nil
	}
	currentSpec := ""
	if pc := selectors.CreateDraftProviderConfig(model); pc != nil {
		currentSpec = strings.TrimSpace(pc.ProviderSpec) // trimlowerlint:allow boundary canonicalization
	}
	items := make([]views.FilterablePickerItem, 0, len(options))
	for _, option := range options {
		spec := strings.TrimSpace(option.Spec) // trimlowerlint:allow boundary canonicalization
		label := providerDisplayName(spec)
		if strings.TrimSpace(label) == "" || strings.EqualFold(label, "Provider") { // trimlowerlint:allow boundary canonicalization
			label = selectors.EmptyOr(strings.TrimSpace(option.Label), spec) // trimlowerlint:allow boundary canonicalization
		}
		choiceSpec := spec
		items = append(items, views.FilterablePickerItem{
			Label:    label,
			Search:   choiceSpec + " " + label,
			Selected: choiceSpec == currentSpec,
			OnChoose: func() []update.Action {
				actions := []update.Action{state.SetCreateDraftProviderSpec{ProviderSpec: choiceSpec}}
				if onCancel != nil {
					actions = append(actions, onCancel()...)
				}
				return actions
			},
		})
	}
	return items
}

func createProviderPropertyRows(
	endpointName string,
	providerConfig *state.ProviderConfigSnapshot,
	createMode bool,
	model state.Model,
) []retained.ViewSpec[state.Model] {
	rows := make([]retained.ViewSpec[state.Model], 0, 8)
	rows = appendCanonicalProviderConfigRows(rows, "", canonicalProviderConfigRows{
		Provider: providerSpecRow(providerConfig),
		Frame: providerFrameChoiceRow(providerFrameChoiceRowSpec{
			ProviderConfig: providerConfig,
			EndpointName:   endpointName,
			CreateMode:     createMode,
		}),
		Model: providerModelChoiceRow(providerModelChoiceRowSpec{
			ProviderConfig: providerConfig,
			EndpointName:   endpointName,
			CreateMode:     createMode,
		}),
	})
	rows = append(rows, retained.Named[state.Model]("alias", providerTargetAliasRow(providerTargetAliasRowSpec{
			ProviderConfig: providerConfig,
			EndpointName:   endpointName,
			CreateMode:     createMode,
		})))
	if providerConfig != nil {
		if providerCredentialSelectionRequired(providerConfig.ProviderSpec, providerConfig.BaseURL, providerConfig.CredentialRef) {
			rows = append(rows, retained.Named[state.Model]("credential", providerCredentialChoiceRow(providerCredentialChoiceRowSpec{
				ProviderConfig: providerConfig,
				EndpointName:   endpointName,
				CreateMode:     createMode,
			})))
		}
	}
	if !createMode &&
		providerConfig != nil &&
		strings.EqualFold(strings.TrimSpace(providerConfig.ProviderSpec), "chatgpt") && // trimlowerlint:allow boundary canonicalization
		providerAuthSession(model, endpointName, providerConfig).URL != "" {
		rows = append(rows, retained.Named[state.Model]("provider-login", providerLoginURLRow(endpointName, providerConfig)))
	}
	if providerConfig != nil && strings.EqualFold(credentialSource(providerConfig.CredentialRef), "env") {
		rows = append(rows, retained.Named[state.Model]("env-key", providerEnvKeyRow(providerEnvKeyRowSpec{
			ProviderConfig: providerConfig,
			EndpointName:   endpointName,
			CreateMode:     createMode,
		})))
	}
	if providerConfig != nil && strings.EqualFold(credentialSource(providerConfig.CredentialRef), "file") {
		rows = append(rows, retained.Named[state.Model]("credential-file", providerCredentialFileBrowseRow(providerCredentialFileBrowseRowSpec{
			ProviderConfig: providerConfig,
			EndpointName:   endpointName,
			CreateMode:     createMode,
		})))
	}
	if providerConfig != nil && strings.TrimSpace(providerConfig.ProviderSpec) == "openai_compatible" { // trimlowerlint:allow boundary canonicalization
		rows = append(rows, retained.Named[state.Model]("backend-url", providerBackendURLRow(providerBackendURLRowSpec{
			ProviderConfig: providerConfig,
			EndpointName:   endpointName,
			CreateMode:     createMode,
		})))
	}
	if !createMode && providerConfig != nil {
		rows = append(rows,
			retained.Named[state.Model]("delete", providerDeleteRow(endpointName, providerConfig)),
		)
	}
	return rows
}

func providerLoginURLRow(endpointName string, providerConfig *state.ProviderConfigSnapshot) retained.ViewSpec[state.Model] {
	return retained.Build[state.Model](func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		if providerConfig == nil {
			return views.RowStatic("login", "not available")
		}
		model := ctx.Model()
		auth := providerAuthSession(model, endpointName, providerConfig)
		loginURL := strings.TrimSpace(auth.URL) // trimlowerlint:allow boundary canonicalization
		summary := "pending browser auth"
		if s := strings.TrimSpace(auth.SessionState); s != "" { // trimlowerlint:allow boundary canonicalization
			summary = "login " + s
		}
		if strings.TrimSpace(auth.SessionError) != "" { // trimlowerlint:allow boundary canonicalization
			summary = "login error"
		}
		return views.RowActionWithCancel(
			"login",
			summary,
			"open",
			func() []update.Action {
				return []update.Action{
					state.OpenSupportLinkRequested{
						Label: "login",
						URL:   loginURL,
					},
				}
			},
			nil,
		)
	})
}

func providerAuthSession(model state.Model, endpointName string, providerConfig *state.ProviderConfigSnapshot) stateModel.AuthSessionView {
	if providerConfig == nil {
		return stateModel.AuthSessionView{}
	}
	ownerKey := stateModel.EndpointProviderAuthOwnerKey(strings.TrimSpace(endpointName), strings.TrimSpace(providerConfig.Ref)).String() // trimlowerlint:allow boundary canonicalization
	if model.AuthSessions == nil {
		return stateModel.AuthSessionView{}
	}
	return model.AuthSessions[strings.TrimSpace(ownerKey)] // trimlowerlint:allow boundary canonicalization
}

func newProviderSummaryRow(provider state.ProviderConfigSnapshot, selected, expanded bool, onActivate func() []update.Action, onCancel func() []update.Action) retained.ViewSpec[state.Model] {
	return retained.Build[state.Model](func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		return providerSummaryRow(ctx, provider, selected, expanded, onActivate, onCancel)
	})
}

func providerSummaryRow(
	_ *retained.Context[state.Model],
	provider state.ProviderConfigSnapshot,
	_ bool,
	expanded bool,
	onActivate func() []update.Action,
	onCancel func() []update.Action,
) retained.ViewSpec[state.Model] {
	label := providerDisplayLabel(provider)
	verb := "edit"
	if expanded {
		verb = "close"
	}
	// Provider/model identifiers can be long; keep declarative row grammar and
	// place them in wide value column with an explicit label for alignment.
	return views.RowActionWideValueWithCancel("model", label, verb, onActivate, onCancel)
}

func providerDisplayLabel(pc state.ProviderConfigSnapshot) string {
	return providerHumanIdentifier(pc)
}
