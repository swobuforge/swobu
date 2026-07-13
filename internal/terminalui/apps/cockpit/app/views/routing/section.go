// Routing section entry: mode-aware section for create vs workspace.
package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// BuildSection is the top-level routing section builder.
// Model-creation row grammar is documented in model_creation_flow.md.
// It routes to create or workspace modes based on whether an endpoint is selected.
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
	provider := model.CreateDraftProviderConfig.ProviderSpec
	providerProtocol := model.CreateDraftProviderConfig.ProviderProtocol
	modelID := model.CreateDraftProviderConfig.ModelID
	cred := model.CreateDraftProviderConfig.CredentialRef
	baseURL := effectiveCreateDraftBaseURL(model, provider)
	credSummary := firstRunCredentialSummary(provider, baseURL, cred)

	runPickerOpen, setRunPickerOpen := retained.UseState(ctx, func() bool { return false })
	pickerState, setPickerState := retained.UseState(ctx, func() views.FilterablePickerState { return views.DefaultFilterablePickerState() })
	keyPickerState, setKeyPickerState := retained.UseState(ctx, func() string { return "" })
	modelPickerOpen, setModelPickerOpen := retained.UseState(ctx, func() bool { return false })
	closeCreateTransients := func() {
		setRunPickerOpen(false)
		setKeyPickerState("")
		setModelPickerOpen(false)
	}

	runOn := buildCreateRunOnRow(ctx, provider, providerProtocol, model.CreateDraftProviderConfig.AuthHeader, runPickerOpen, setRunPickerOpen, pickerState, setPickerState, closeCreateTransients)
	rows := []retained.ViewSpec[state.Model]{retained.Named[state.Model]("run_on", runOn)}

	flow := state.EvaluateCreateDraftRouteSetup(model.CreateDraftProviderConfig)
	if flow.CredentialVisible {
		useKeyFrom := buildCreateUseKeyFromRow(ctx, provider, providerProtocol, model.CreateDraftProviderConfig.AuthHeader, credSummary, baseURL, cred, keyPickerState, setKeyPickerState, pickerState, setPickerState, closeCreateTransients)
		rows = append(rows, retained.Named[state.Model]("use_key_from", useKeyFrom))
	}
	if strings.EqualFold(provider, "bedrock") { // swobu:io-string source=boundary
		rows = append(rows, retained.Named[state.Model]("profile", bedrockAuthProfileEditor(bedrockAuthProfileEditorSpec{
			ProviderConfig: &model.CreateDraftProviderConfig,
			CreateMode:     true,
		})))
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
	if authHeaderRow := providerAuthHeaderRow(providerAuthHeaderRowSpec{
		ProviderConfig: &model.CreateDraftProviderConfig,
		CreateMode:     true,
	}); authHeaderRow != nil {
		rows = append(rows, retained.Named[state.Model]("auth-header", authHeaderRow))
	}
	modelRow := buildCreateModelRow(ctx, modelPickerOpen, setModelPickerOpen, pickerState, setPickerState, closeCreateTransients)
	rows = append(rows, retained.Named[state.Model]("model", modelRow))
	if strings.TrimSpace(provider) != "" && strings.TrimSpace(modelID) != "" { // swobu:io-string source=boundary
		rows = append(rows, retained.Named[state.Model]("id", aliasInlineEditorRow(
			ctx,
			selectors.EmptyOr(model.CreateDraftProviderConfig.TargetAlias, views.ValueAuto),
			model.CreateDraftProviderConfig.TargetAlias,
			"fast",
			func(value string) []update.Action {
				return []update.Action{state.SetCreateDraftTargetAlias{TargetAlias: value}}
			},
		)))
	}
	rows = append(rows, retained.Named[state.Model]("protocol", createDraftProtocolModeRow(model)))
	rows = append(rows, retained.Named[state.Model]("create", createDraftTestOrCreateRow(model)))

	summary := createSectionSummary(provider, modelID, credSummary)
	return retained.Named[state.Model](
		"routing-create",
		views.NewCollapsibleSection(
			views.SectionRouting,
			// Keep routing disclosure explicit: only Enter opens it.
			false,
			"choose",
			views.SummaryRow(summary),
			rows...,
		),
	)
}

func buildCreateRunOnRow(
	ctx *retained.Context[state.Model],
	provider string,
	providerProtocol string,
	authHeader string,
	runPickerOpen bool,
	setRunPickerOpen func(bool),
	pickerState views.FilterablePickerState,
	setPickerState func(views.FilterablePickerState),
	closeCreateTransients func(),
) retained.ViewSpec[state.Model] {
	onCancel := func() []update.Action {
		setRunPickerOpen(false)
		return []update.Action{
			state.SetInteractionMode{Mode: state.InteractionModeNAV},
			interaction.FocusKeyAction{Key: "run_on"},
		}
	}
	items := createRunOnChoiceItems(ctx.Model(), onCancel)
	useDeferredFocusOnOpen(ctx, runPickerOpen, views.FilterablePickerFirstFocusKey(items, views.FilterablePickerConfig{KeyPrefix: "run-on-provider-option"}))
	runOn := views.RowChoiceWithHooks(views.RowRunOn, firstRunRunOnSummary(provider), func() []update.Action {
		nextOpen := !runPickerOpen
		if nextOpen {
			closeCreateTransients()
		}
		setRunPickerOpen(nextOpen)
		if nextOpen {
			views.ResetFilterablePickerState(setPickerState)
		}
		mode := state.InteractionModeNAV
		if nextOpen {
			mode = state.InteractionModePickOne
		}
		actions := []update.Action{state.SetInteractionMode{Mode: mode}}
		return actions
	}, nil, views.FocusAffordance("choose", false))
	if !runPickerOpen {
		return runOn
	}

	options := state.ProviderOptions()
	providerItems := make([]views.FilterablePickerItem, 0, len(options))
	for _, option := range options {
		specChoice := strings.TrimSpace(option.Spec) // swobu:io-string source=boundary
		if specChoice == "" {
			continue
		}
		label := strings.TrimSpace(providerDisplayName(specChoice)) // swobu:io-string source=boundary
		if label == "" || strings.EqualFold(label, "Provider") {
			label = selectors.EmptyOr(strings.TrimSpace(option.Label), specChoice) // swobu:io-string source=boundary
		}
		providerItems = append(providerItems, views.FilterablePickerItem{
			Label:  label,
			Search: specChoice + " " + label,
			OnChoose: func() []update.Action {
				setRunPickerOpen(false)
				nextBaseURL := strings.TrimSpace(profile.DefaultExecuteBaseURL(specChoice)) // swobu:io-string source=boundary
				if strings.EqualFold(specChoice, "bedrock") && nextBaseURL == "" {
					if region := strings.TrimSpace(bedrockRegionFromEnv()); region != "" { // swobu:io-string source=boundary
						nextBaseURL = bedrockBaseURLForRegion(region)
					}
				}
				nextMode := profile.ProviderProtocolAuto
				return []update.Action{
					state.SetCreateDraftProviderSpec{ProviderSpec: specChoice},
					state.SetCreateDraftCredentialRef{CredentialRef: ""},
					state.SetCreateDraftModelIDAction{ModelID: ""},
					state.LoadRoutingModelCatalogRequestedAction{
						Scope:            state.RoutingModelCatalogScopeCreateDraft,
						ProviderSpec:     specChoice,
						AuthHeader:       strings.TrimSpace(authHeader), // swobu:io-string source=boundary
						ProviderProtocol: strings.TrimSpace(nextMode),   // swobu:io-string source=boundary
						BaseURL:          nextBaseURL,
						CredentialRef:    "",
					},
					state.SetInteractionMode{Mode: state.InteractionModeNAV},
					interaction.FocusKeyAction{Key: "run_on"},
				}
			},
		})
	}
	return views.RenderFilterablePickerDisclosure(ctx, runOn, pickerState, setPickerState, providerItems, views.FilterablePickerConfig{
		KeyPrefix:      "run-on-provider-option",
		BuildOptionRow: views.ChoicePickerOptionRow(false),
		WindowSize:     6,
		FindLabel:      "find",
		OnNoMatchFocus: func() []update.Action { return []update.Action{interaction.FocusKeyAction{Key: "run_on"}} },
		OnCancel:       onCancel,
	})
}

func buildCreateUseKeyFromRow(
	ctx *retained.Context[state.Model],
	provider string,
	providerProtocol string,
	authHeader string,
	credSummary string,
	baseURL string,
	credentialRef string,
	keyPickerState string,
	setKeyPickerState func(string),
	pickerState views.FilterablePickerState,
	setPickerState func(views.FilterablePickerState),
	closeCreateTransients func(),
) retained.ViewSpec[state.Model] {
	if provider == "" {
		return views.RowChoiceWithHooks(views.RowUseKeyFrom, credSummary, func() []update.Action { return nil }, nil, views.FocusAffordance("choose", false))
	}
	if !state.CreateDraftCredentialStrategySelectable(provider) {
		return views.RowStatic(views.RowUseKeyFrom, credSummary)
	}
	items := credentialOptionItems(credentialSource(credentialRef), func(choice credentialChoiceOption) []update.Action {
		actions := applyProviderCredentialSelection(choice.Mode, provider, nil, "", true)
		nextRef := createDraftCredentialRefFromActions(actions)
		setKeyPickerState("")
		if profile.IsInteractiveAuthMode(choice.Mode) {
			draft := createDraftAuthProviderConfig(provider, baseURL, nextRef)
			if choice.Mode == profile.AuthModeChatGPTLogin {
				actions = append(actions, state.ResetAuthSessionUIRequestedAction{})
			}
			if choice.Mode == profile.AuthModeChatGPTDeviceAuth {
				actions = append(actions, startAuthActionsForCreateDraft(draft)...)
			}
		}
		actions = append(actions,
			state.SetCreateDraftModelIDAction{ModelID: ""},
			state.LoadRoutingModelCatalogRequestedAction{
				Scope:            state.RoutingModelCatalogScopeCreateDraft,
				ProviderSpec:     provider,
				AuthHeader:       strings.TrimSpace(authHeader),       // swobu:io-string source=boundary
				ProviderProtocol: strings.TrimSpace(providerProtocol), // swobu:io-string source=boundary
				BaseURL:          baseURL,
				CredentialRef:    nextRef,
			},
			state.SetInteractionMode{Mode: state.InteractionModeNAV},
		)
		focusKey := "use_key_from"
		if choice.Mode == profile.AuthModeKeychain {
			focusKey = "keychain"
		}
		actions = append(actions, interaction.FocusKeyAction{Key: focusKey})
		return actions
	}, provider)
	useKeyFrom := views.RowChoiceWithHooks(views.RowUseKeyFrom, credSummary, func() []update.Action {
		if provider == "" {
			return nil
		}
		closeCreateTransients()
		setKeyPickerState("source-open")
		views.ResetFilterablePickerState(setPickerState)
		actions := []update.Action{
			state.SetInteractionMode{Mode: state.InteractionModePickOne},
		}
		if focusKey := views.FilterablePickerFirstFocusKey(items, views.FilterablePickerConfig{KeyPrefix: "create-credential-source-option"}); focusKey != "" {
			actions = append(actions, interaction.FocusKeyAction{Key: focusKey})
		}
		return actions
	}, nil, views.FocusAffordance("choose", false))
	if strings.TrimSpace(keyPickerState) != "source-open" { // swobu:io-string source=boundary
		return useKeyFrom
	}
	return views.RenderFilterablePickerDisclosure(ctx, useKeyFrom, pickerState, setPickerState, items, views.FilterablePickerConfig{
		KeyPrefix:      "create-credential-source-option",
		BuildOptionRow: views.ChoicePickerOptionRow(true),
		WindowSize:     6,
		NonFilterable:  true,
		OnCancel: func() []update.Action {
			setKeyPickerState("")
			return []update.Action{
				state.SetInteractionMode{Mode: state.InteractionModeNAV},
				interaction.FocusKeyAction{Key: "use_key_from"},
			}
		},
	})
}

func buildCreateInteractiveAuthRows(model state.Model) []retained.ViewSpec[state.Model] {
	provider := model.CreateDraftProviderConfig.ProviderSpec
	source := credentialSource(model.CreateDraftProviderConfig.CredentialRef)
	mode := profile.AuthMode(strings.ToLower(source)) // swobu:io-string source=boundary
	if !profile.SupportsAuthMode(provider, mode) || !profile.IsInteractiveAuthMode(mode) {
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
		Mode:         mode,
		StartAuth: func(next state.ProviderConfigSnapshot) []update.Action {
			return startAuthActionsForCreateDraft(next)
		},
		SwitchToDeviceAuth: func(next state.ProviderConfigSnapshot) []update.Action {
			next.CredentialRef = string(profile.AuthModeChatGPTDeviceAuth)
			actions := []update.Action{
				state.SetCreateDraftCredentialRef{CredentialRef: string(profile.AuthModeChatGPTDeviceAuth)},
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
	closeCreateTransients func(),
) retained.ViewSpec[state.Model] {
	return buildDraftModelChoiceRow(ctx, draftModelRowSpec{
		Binding:        createDraftModelBinding{},
		PickerOpen:     modelPickerOpen,
		SetPickerOpen:  setModelPickerOpen,
		PickerState:    pickerState,
		SetPickerState: setPickerState,
		KeyPrefix:      "create-model-option",
		FocusKey:       "model",
		OnOpen:         closeCreateTransients,
	})
}

func workspaceSection(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
	model := ctx.Model()
	snapshot := selectors.CurrentEndpointSnapshot(model)
	if snapshot == nil {
		return views.Section(views.SectionRouting, views.RowStatic("", "not selected"))
	}
	provider := selectors.SelectedProviderConfig(model, snapshot)
	if provider == nil {
		return views.Section(views.SectionRouting, views.RowStatic("", "not selected"))
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
