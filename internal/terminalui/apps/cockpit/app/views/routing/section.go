// Routing section entry: mode-aware section for create vs workspace.
package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// BuildSection is the top-level routing section builder.
// Model-creation row grammar is documented in model_creation_flow.md.
// It routes to create or workspace variants based on whether an endpoint is selected.
func BuildSection(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	var out retained.ViewSpec[state.Model]
	if model.CurrentEndpoint == "" {
		out = createSection(ctx)
	} else {
		out = workspaceSection(ctx)
	}
	return out
}

func createSection(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	nameSet := model.CreateDraftName != ""
	provider := model.CreateDraftProviderConfig.ProviderSpec
	modelID := model.CreateDraftProviderConfig.ModelID
	cred := model.CreateDraftProviderConfig.CredentialRef
	baseURL := effectiveCreateDraftBaseURL(model, provider)
	credSummary := firstRunCredentialSummary(provider, baseURL, cred)

	defaultOpen := provider != "" || nameSet
	runPickerOpen, setRunPickerOpen := retained.UseState(ctx, func() bool { return false })
	pickerState, setPickerState := retained.UseState(ctx, func() views.FilterablePickerState { return views.DefaultFilterablePickerState() })
	keyPickerState, setKeyPickerState := retained.UseState(ctx, func() string { return "" })
	modelPickerOpen, setModelPickerOpen := retained.UseState(ctx, func() bool { return false })

	runOn := buildCreateRunOnRow(ctx, provider, runPickerOpen, setRunPickerOpen, pickerState, setPickerState)
	rows := []retained.ViewSpec[state.Model]{retained.Named[state.Model]("run_on", runOn)}

	flow := state.EvaluateCreateDraftRouteSetup(model.CreateDraftProviderConfig)
	if flow.CredentialVisible {
		useKeyFrom := buildCreateUseKeyFromRow(provider, credSummary, baseURL, cred, keyPickerState, setKeyPickerState)
		rows = append(rows, retained.Named[state.Model]("use_key_from", useKeyFrom))
	}
	if flow.ScopeVisible {
		rows = append(rows, retained.Named[state.Model]("scope", providerScopeRow(providerScopeRowSpec{
			ProviderConfig: &model.CreateDraftProviderConfig,
			CreateMode:     true,
		})))
	}
	rows = appendCreateCredentialRows(rows, provider, cred)
	// Dependency actions (for example auth start/continue) must render before
	// model so operators see and satisfy prerequisites in-order.
	rows = append(rows, buildCreateInteractiveAuthRows(model)...)
	modelRow := buildCreateModelRow(ctx, modelPickerOpen, setModelPickerOpen, pickerState, setPickerState)
	rows = append(rows, retained.Named[state.Model]("model", modelRow))
	rows = append(rows, retained.Named[state.Model]("delivery", providerFrameChoiceRow(providerFrameChoiceRowSpec{
		ProviderConfig: &model.CreateDraftProviderConfig,
		CreateMode:     true,
	})))

	summary := createSectionSummary(provider, modelID, credSummary)
	return retained.Named[state.Model](
		"routing-create",
		views.NewCollapsibleSection(
			views.SectionRouting,
			defaultOpen,
			"choose",
			views.SummaryRow(summary),
			rows...,
		),
	)
}

func buildCreateRunOnRow(
	ctx *retained.Context[state.Model],
	provider string,
	runPickerOpen bool,
	setRunPickerOpen func(bool),
	pickerState views.FilterablePickerState,
	setPickerState func(views.FilterablePickerState),
) retained.ViewSpec[state.Model] {
	runOn := views.RowChoiceWithHooks(views.RowRunOn, firstRunRunOnSummary(provider), func() []update.Action {
		nextOpen := !runPickerOpen
		setRunPickerOpen(nextOpen)
		if nextOpen {
			views.ResetFilterablePickerState(setPickerState)
		}
		mode := state.InteractionModeNAV
		if nextOpen {
			mode = state.InteractionModePickOne
		}
		actions := []update.Action{state.SetInteractionMode{Mode: mode}}
		if nextOpen {
			actions = append(actions, interaction.FocusKeyAction{Key: views.FilterablePickerFocusKey("run-on-provider-option", 0)})
		}
		return actions
	}, nil, views.FocusAffordance("choose", false))
	if !runPickerOpen {
		return runOn
	}

	options := state.ProviderOptions()
	items := make([]views.FilterablePickerItem, 0, len(options))
	for _, option := range options {
		specChoice := strings.TrimSpace(option.Spec) // swobu:io-string source=boundary
		if specChoice == "" {
			continue
		}
		label := strings.TrimSpace(providerDisplayName(specChoice)) // swobu:io-string source=boundary
		if label == "" || strings.EqualFold(label, "Provider") {
			label = selectors.EmptyOr(strings.TrimSpace(option.Label), specChoice) // swobu:io-string source=boundary
		}
		items = append(items, views.FilterablePickerItem{
			Label:  label,
			Search: specChoice + " " + label,
			OnChoose: func() []update.Action {
				setRunPickerOpen(false)
				nextBaseURL := strings.TrimSpace(providercatalog.DefaultExecuteBaseURL(specChoice)) // swobu:io-string source=boundary
				if strings.EqualFold(specChoice, "bedrock") && nextBaseURL == "" {
					if region := strings.TrimSpace(bedrockRegionFromEnv()); region != "" { // swobu:io-string source=boundary
						nextBaseURL = bedrockBaseURLForRegion(region)
					}
				}
				return []update.Action{
					state.SetCreateDraftProviderSpec{ProviderSpec: specChoice},
					state.SetCreateDraftCredentialRef{CredentialRef: ""},
					state.SetCreateDraftModelIDAction{ModelID: ""},
					state.LoadRoutingModelCatalogRequestedAction{
						Scope:         state.RoutingModelCatalogScopeCreateDraft,
						ProviderSpec:  specChoice,
						BaseURL:       nextBaseURL,
						CredentialRef: "",
					},
					state.SetInteractionMode{Mode: state.InteractionModeNAV},
					interaction.FocusKeyAction{Key: "run_on"},
				}
			},
		})
	}
	return views.RenderFilterablePickerDisclosure(ctx, runOn, pickerState, setPickerState, items, views.FilterablePickerConfig{
		KeyPrefix:      "run-on-provider-option",
		BuildOptionRow: views.ChoicePickerOptionRow(false),
		WindowSize:     6,
		FindLabel:      "find",
		OnNoMatchFocus: func() []update.Action { return []update.Action{interaction.FocusKeyAction{Key: "run_on"}} },
		OnCancel: func() []update.Action {
			setRunPickerOpen(false)
			return []update.Action{
				state.SetInteractionMode{Mode: state.InteractionModeNAV},
				interaction.FocusKeyAction{Key: "run_on"},
			}
		},
	})
}

func buildCreateUseKeyFromRow(
	provider string,
	credSummary string,
	baseURL string,
	credentialRef string,
	keyPickerState string,
	setKeyPickerState func(string),
) retained.ViewSpec[state.Model] {
	if provider == "" {
		return views.RowChoiceWithHooks(views.RowUseKeyFrom, credSummary, func() []update.Action { return nil }, nil, views.FocusAffordance("choose", false))
	}
	if !state.CreateDraftCredentialStrategySelectable(provider) {
		return views.RowStatic(views.RowUseKeyFrom, credSummary)
	}
	useKeyFrom := views.RowChoiceWithHooks(views.RowUseKeyFrom, credSummary, func() []update.Action {
		if provider == "" {
			return nil
		}
		setKeyPickerState("source-open")
		return []update.Action{state.SetInteractionMode{Mode: state.InteractionModePickOne}}
	}, nil, views.FocusAffordance("choose", false))
	if strings.TrimSpace(keyPickerState) != "source-open" { // swobu:io-string source=boundary
		return useKeyFrom
	}
	options := credentialOptionRows(credentialSource(credentialRef), func(choice string) []update.Action {
		actions := applyProviderCredentialSelection(choice, provider, nil, "", true)
		nextRef := createDraftCredentialRefFromActions(actions)
		setKeyPickerState("")
		variant := providercatalog.AuthVariant(strings.ToLower(strings.TrimSpace(choice))) // swobu:io-string source=boundary
		if providercatalog.IsInteractiveAuthVariant(variant) {
			draft := createDraftAuthProviderConfig(provider, baseURL, nextRef)
			if variant == providercatalog.AuthVariantChatGPTLogin {
				actions = append(actions, state.ResetAuthSessionUIRequestedAction{})
			}
			if variant == providercatalog.AuthVariantChatGPTDeviceAuth {
				actions = append(actions, startAuthActionsForCreateDraft(draft)...)
			}
		}
		actions = append(actions,
			state.SetCreateDraftModelIDAction{ModelID: ""},
			state.LoadRoutingModelCatalogRequestedAction{
				Scope:         state.RoutingModelCatalogScopeCreateDraft,
				ProviderSpec:  provider,
				BaseURL:       baseURL,
				CredentialRef: nextRef,
			},
			state.SetInteractionMode{Mode: state.InteractionModeNAV},
			interaction.FocusKeyAction{Key: "use_key_from"},
		)
		return actions
	}, func() []update.Action {
		setKeyPickerState("")
		return []update.Action{
			state.SetInteractionMode{Mode: state.InteractionModeNAV},
			interaction.FocusKeyAction{Key: "use_key_from"},
		}
	}, provider, true)
	return toolkitviews.NewAnchoredDisclosure(useKeyFrom, options...)
}

func buildCreateInteractiveAuthRows(model state.Model) []retained.ViewSpec[state.Model] {
	provider := model.CreateDraftProviderConfig.ProviderSpec
	source := credentialSource(model.CreateDraftProviderConfig.CredentialRef)
	variant := providercatalog.AuthVariant(strings.ToLower(source)) // swobu:io-string source=boundary
	if !providercatalog.SupportsAuthVariant(provider, variant) || !providercatalog.IsInteractiveAuthVariant(variant) {
		return nil
	}
	draft := createDraftAuthProviderConfig(
		provider,
		effectiveCreateDraftBaseURL(model, provider),
		model.CreateDraftProviderConfig.CredentialRef,
	)
	return interactiveAuthStatusRows(model, interactiveAuthRenderConfig{
		EndpointName: "",
		Draft:        draft,
		Variant:      variant,
		StartAuth: func(next state.ProviderConfigSnapshot) []update.Action {
			return startAuthActionsForCreateDraft(next)
		},
		SwitchToDeviceAuth: func(next state.ProviderConfigSnapshot) []update.Action {
			next.CredentialRef = string(providercatalog.AuthVariantChatGPTDeviceAuth)
			actions := []update.Action{
				state.SetCreateDraftCredentialRef{CredentialRef: string(providercatalog.AuthVariantChatGPTDeviceAuth)},
				state.ResetAuthSessionUIRequestedAction{},
			}
			return append(actions, startAuthActionsForCreateDraft(next)...)
		},
	})
}

func createDraftAuthProviderConfig(provider, baseURL, credentialRef string) state.ProviderConfigSnapshot {
	return state.ProviderConfigSnapshot{
		Ref:           "create-draft",
		ProviderSpec:  strings.TrimSpace(provider),      // swobu:io-string source=boundary
		BaseURL:       strings.TrimSpace(baseURL),       // swobu:io-string source=boundary
		CredentialRef: strings.TrimSpace(credentialRef), // swobu:io-string source=boundary // swobu:io-string source=boundary
	}
}

func appendCreateCredentialRows(rows []retained.ViewSpec[state.Model], provider string, credentialRef string) []retained.ViewSpec[state.Model] {
	if provider == "" {
		return rows
	}
	if isResolvedInteractiveCredential(provider, credentialRef) {
		return rows
	}
	rows = append(rows, authModeRendererForCredentialRef(credentialRef).RenderCreateExtras(provider, credentialRef)...)
	return rows
}

func buildCreateModelRow(
	ctx *retained.Context[state.Model],
	modelPickerOpen bool,
	setModelPickerOpen func(bool),
	pickerState views.FilterablePickerState,
	setPickerState func(views.FilterablePickerState),
) retained.ViewSpec[state.Model] {
	return buildDraftModelChoiceRow(ctx, draftModelRowSpec{
		Binding:        createDraftModelBinding{},
		PickerOpen:     modelPickerOpen,
		SetPickerOpen:  setModelPickerOpen,
		PickerState:    pickerState,
		SetPickerState: setPickerState,
		KeyPrefix:      "create-model-option",
		FocusKey:       "model",
	})
}

func workspaceSection(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	snapshot := selectors.CurrentEndpointSnapshot(model)
	if snapshot == nil {
		return views.Section[state.Model](views.SectionRouting, views.RowStatic("", "not selected"))
	}
	provider := selectors.SelectedProviderConfig(model, snapshot)
	if provider == nil {
		return views.Section[state.Model](views.SectionRouting, views.RowStatic("", "not selected"))
	}
	summary := workspaceRoutingSummary(*provider)
	if model.HeaderStatus == "saved" {
		return views.NewCollapsibleSection(
			views.SectionRouting,
			false,
			"open",
			views.SummaryRow(savedRoutingSummary(*provider)),
		)
	}
	return views.NewCollapsibleSection(
		views.SectionRouting,
		false,
		"choose",
		views.SummaryRow(summary),
		retained.Named[state.Model]("run_on", retained.Build[state.Model](BuildRunOnWorkspaceRow)),
		retained.Named[state.Model]("providers", retained.Build[state.Model](BuildProvidersWorkspacePanel)),
	)
}
