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

	useKeyFrom := buildCreateUseKeyFromRow(provider, credSummary, baseURL, cred, keyPickerState, setKeyPickerState)
	rows = append(rows, retained.Named[state.Model]("use_key_from", useKeyFrom))

	rows = appendCreateCredentialRows(rows, provider, credentialSource(cred))
	modelRow := buildCreateModelRow(ctx, modelPickerOpen, setModelPickerOpen, pickerState, setPickerState)
	rows = append(rows, retained.Named[state.Model]("model", modelRow))

	summary := firstRunRunOnSummary(provider)
	if provider != "" {
		summary = providercatalog.DisplayName(provider) + " · " + selectors.EmptyOr(credentialSource(cred), "not set")
		if modelID != "" {
			summary += " · " + modelID
		}
	}
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

	items := make([]views.FilterablePickerItem, 0, 5)
	for _, spec := range []string{"openai", "openrouter", "anthropic", "ollama", "custom"} {
		specChoice := spec
		items = append(items, views.FilterablePickerItem{
			Label:  providercatalog.DisplayName(specChoice),
			Search: specChoice + " " + providercatalog.DisplayName(specChoice),
			OnChoose: func() []update.Action {
				setRunPickerOpen(false)
				nextBaseURL := strings.TrimSpace(providercatalog.DefaultBaseURL(specChoice))
				return []update.Action{
					state.SetCreateDraftProviderSpec{ProviderSpec: specChoice},
					state.SetCreateDraftCredentialRef{CredentialRef: ""},
					state.SetCreateDraftModelID{ModelID: ""},
					state.LoadRoutingModelCatalogRequested{
						Scope:         state.RoutingModelCatalogScopeCreateDraft,
						ProviderSpec:  specChoice,
						BaseURL:       nextBaseURL,
						CredentialRef: "",
						ProtocolKind:  defaultProtocolKindForProvider(specChoice),
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
	if !providerCredentialSelectionRequired(provider, baseURL, credentialRef) {
		return views.RowStatic(views.RowUseKeyFrom, credSummary)
	}
	useKeyFrom := views.RowChoiceWithHooks(views.RowUseKeyFrom, credSummary, func() []update.Action {
		if provider == "" {
			return nil
		}
		setKeyPickerState("source-open")
		return []update.Action{state.SetInteractionMode{Mode: state.InteractionModePickOne}}
	}, nil, views.FocusAffordance("choose", false))
	if strings.TrimSpace(keyPickerState) != "source-open" {
		return useKeyFrom
	}

	optionRows := make([]retained.ViewSpec[state.Model], 0, 3)
	for _, choice := range []string{"env", "keychain", "file"} {
		keySource := choice
		optionRows = append(optionRows, firstRunProviderChoiceRow(keySource, func() []update.Action {
			nextRef := keySource
			if keySource == "env" {
				nextRef = encodeCredentialEnvRef(providercatalog.DefaultEnvKeyForSpec(provider))
			}
			if keySource == "file" {
				nextRef = encodeCredentialFileRef("")
			}
			setKeyPickerState("")
			return []update.Action{
				state.SetCreateDraftCredentialRef{CredentialRef: nextRef},
				state.SetCreateDraftModelID{ModelID: ""},
				state.LoadRoutingModelCatalogRequested{
					Scope:         state.RoutingModelCatalogScopeCreateDraft,
					ProviderSpec:  provider,
					BaseURL:       baseURL,
					CredentialRef: nextRef,
					ProtocolKind:  defaultProtocolKindForProvider(provider),
				},
				state.SetInteractionMode{Mode: state.InteractionModeNAV},
				interaction.FocusKeyAction{Key: "use_key_from"},
			}
		}))
	}
	return toolkitviews.NewAnchoredDisclosure(useKeyFrom, optionRows...)
}

func appendCreateCredentialRows(rows []retained.ViewSpec[state.Model], provider string, credSource string) []retained.ViewSpec[state.Model] {
	if provider == "" {
		return rows
	}
	if strings.EqualFold(credSource, "env") {
		rows = append(rows, retained.Named[state.Model]("env-key", providerEnvKeyRow(providerEnvKeyRowSpec{CreateMode: true})))
	}
	if strings.EqualFold(credSource, "keychain") {
		rows = append(rows, retained.Named[state.Model]("keychain-key-name", providerKeychainKeyNameRow(providerKeychainKeyNameRowSpec{CreateMode: true})))
	}
	if strings.EqualFold(credSource, "file") {
		rows = append(rows, retained.Named[state.Model]("credential-file", providerCredentialFileBrowseRow(providerCredentialFileBrowseRowSpec{CreateMode: true})))
	}
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

func firstRunProviderChoiceRow(label string, onActivate func() []update.Action) retained.ViewSpec[state.Model] {
	return toolkitviews.ListItemRow[state.Model](
		toolkitviews.InsetLabel(strings.TrimSpace(label), 4),
		false,
		false,
		false,
		onActivate,
		nil,
	)
}

func firstRunRunOnSummary(provider string) string {
	if strings.TrimSpace(provider) == "" {
		return "choose a provider"
	}
	return providercatalog.DisplayName(provider)
}

func firstRunCredentialSummary(provider, baseURL, credentialRef string) string {
	if strings.TrimSpace(provider) == "" {
		return "not set"
	}
	cred := credentialSource(strings.TrimSpace(credentialRef))
	if cred != "" {
		return cred
	}
	if !providerCredentialSelectionRequired(provider, baseURL, "") {
		return "not required"
	}
	return "choose a key source"
}

func savedRoutingSummary(provider state.ProviderConfigSnapshot) string {
	spec := providercatalog.DisplayName(provider.ProviderSpec)
	cred := strings.TrimSpace(provider.CredentialRef)
	if cred == "" {
		cred = defaultCreateDraftCredentialRef(provider.ProviderSpec)
	}
	modelID := providerConfigSummary(provider)
	if modelID == "" && cred == "" {
		return spec
	}
	if modelID == "" {
		return spec + " · " + cred
	}
	if cred == "" {
		return spec + " · " + modelID
	}
	return spec + " · " + cred + " · " + modelID
}

func workspaceRoutingSummary(provider state.ProviderConfigSnapshot) string {
	spec := providercatalog.DisplayName(provider.ProviderSpec)
	modelID := strings.TrimSpace(provider.ModelID)
	if modelID == "" {
		return spec + " · models"
	}
	return spec + " · " + modelID + " · models"
}

func defaultCreateDraftCredentialRef(provider string) string {
	spec := strings.TrimSpace(strings.ToLower(provider))
	if spec == "" {
		return ""
	}
	if !providercatalog.RequiresCredential(spec, providercatalog.DefaultBaseURL(spec)) {
		return ""
	}
	return "env"
}

func effectiveCreateDraftBaseURL(model state.Model, provider string) string {
	baseURL := model.CreateDraftProviderConfig.BaseURL
	if baseURL != "" {
		return baseURL
	}
	return strings.TrimSpace(providercatalog.DefaultBaseURL(provider))
}
