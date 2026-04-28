// Routing section entry: mode-aware section for create vs workspace.
package routing

import (
	"strings"

	"github.com/metrofun/swobu/internal/adapters/inbound/tui/app/selectors"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/app/state"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/app/views"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/interaction"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/update"
	"github.com/metrofun/swobu/internal/adapters/inbound/tui/engine/view"
	toolkitviews "github.com/metrofun/swobu/internal/adapters/inbound/tui/toolkit/views"
	"github.com/metrofun/swobu/internal/domain/providercatalog"
)

// BuildSection is the top-level routing section builder.
// It routes to create or workspace variants based on whether an endpoint is selected.
func BuildSection(ctx *view.Context[state.Model]) view.ViewSpec[state.Model] {
	model := ctx.Model()
	var out view.ViewSpec[state.Model]
	if model.CurrentEndpoint == "" {
		out = createSection(ctx)
	} else {
		out = workspaceSection(ctx)
	}
	return out
}

func createSection(ctx *view.Context[state.Model]) view.ViewSpec[state.Model] {
	model := ctx.Model()
	nameSet := model.CreateDraftName != ""
	provider := model.CreateDraftProviderConfig.ProviderSpec
	modelID := model.CreateDraftProviderConfig.ModelID
	cred := model.CreateDraftProviderConfig.CredentialRef
	baseURL := effectiveCreateDraftBaseURL(model, provider)
	credSummary := firstRunCredentialSummary(provider, baseURL, cred)

	defaultOpen := provider != "" || nameSet
	runPickerOpen, setRunPickerOpen := view.UseState(ctx, func() bool { return false })
	pickerState, setPickerState := view.UseState(ctx, func() views.FilterablePickerState { return views.DefaultFilterablePickerState() })
	keyPickerState, setKeyPickerState := view.UseState(ctx, func() string { return "" })
	modelPickerOpen, setModelPickerOpen := view.UseState(ctx, func() bool { return false })

	runOn := buildCreateRunOnRow(ctx, provider, runPickerOpen, setRunPickerOpen, pickerState, setPickerState)
	rows := []view.ViewSpec[state.Model]{view.Named[state.Model]("run_on", runOn)}

	useKeyFrom := buildCreateUseKeyFromRow(provider, credSummary, baseURL, cred, keyPickerState, setKeyPickerState)
	rows = append(rows, view.Named[state.Model]("use_key_from", useKeyFrom))

	rows = appendCreateCredentialRows(rows, provider, credentialSource(cred))
	modelRow := buildCreateModelRow(ctx, model, provider, modelID, cred, baseURL, modelPickerOpen, setModelPickerOpen, pickerState, setPickerState)
	rows = append(rows, view.Named[state.Model]("model", modelRow))

	summary := firstRunRunOnSummary(provider)
	if provider != "" {
		summary = providercatalog.DisplayName(provider) + " · " + selectors.EmptyOr(credentialSource(cred), "not set")
		if modelID != "" {
			summary += " · " + modelID
		}
	}
	return view.Named[state.Model](
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
	ctx *view.Context[state.Model],
	provider string,
	runPickerOpen bool,
	setRunPickerOpen func(bool),
	pickerState views.FilterablePickerState,
	setPickerState func(views.FilterablePickerState),
) view.ViewSpec[state.Model] {
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
					state.LoadCreateDraftModelCatalogRequested{
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
) view.ViewSpec[state.Model] {
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

	optionRows := make([]view.ViewSpec[state.Model], 0, 3)
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
				state.LoadCreateDraftModelCatalogRequested{
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

func appendCreateCredentialRows(rows []view.ViewSpec[state.Model], provider string, credSource string) []view.ViewSpec[state.Model] {
	if provider == "" {
		return rows
	}
	if strings.EqualFold(credSource, "env") {
		rows = append(rows, view.Named[state.Model]("env-key", providerEnvKeyRow(providerEnvKeyRowSpec{CreateMode: true})))
	}
	if strings.EqualFold(credSource, "keychain") {
		rows = append(rows, view.Named[state.Model]("keychain-key-name", providerKeychainKeyNameRow(providerKeychainKeyNameRowSpec{CreateMode: true})))
	}
	if strings.EqualFold(credSource, "file") {
		rows = append(rows, view.Named[state.Model]("credential-file", providerCredentialFileBrowseRow(providerCredentialFileBrowseRowSpec{CreateMode: true})))
	}
	return rows
}

func buildCreateModelRow(
	ctx *view.Context[state.Model],
	model state.Model,
	provider string,
	modelID string,
	cred string,
	baseURL string,
	modelPickerOpen bool,
	setModelPickerOpen func(bool),
	pickerState views.FilterablePickerState,
	setPickerState func(views.FilterablePickerState),
) view.ViewSpec[state.Model] {
	modelSummary := "not set"
	if modelPickerOpen && modelID == "" {
		modelSummary = "choose a model"
	}
	if modelID != "" {
		modelSummary = modelID
	}
	modelRow := views.RowChoiceWithHooks(views.RowModel, modelSummary, func() []update.Action {
		if provider == "" {
			return nil
		}
		setModelPickerOpen(true)
		views.ResetFilterablePickerState(setPickerState)
		return []update.Action{
			state.LoadCreateDraftModelCatalogRequested{
				ProviderSpec:  provider,
				BaseURL:       baseURL,
				CredentialRef: cred,
				ProtocolKind:  defaultProtocolKindForProvider(provider),
			},
			state.SetInteractionMode{Mode: state.InteractionModePickOne},
			interaction.FocusKeyAction{Key: views.FilterablePickerFocusKey("create-model-option", 0)},
		}
	}, nil, views.FocusAffordance("choose", false))
	if provider == "" || !modelPickerOpen {
		return modelRow
	}

	items := make([]views.FilterablePickerItem, 0, len(model.CreateDraftModelIDs))
	for _, choice := range model.CreateDraftModelIDs {
		modelChoice := choice
		items = append(items, views.FilterablePickerItem{
			Label: modelChoice,
			OnChoose: func() []update.Action {
				setModelPickerOpen(false)
				return []update.Action{
					state.SetCreateDraftModelID{ModelID: modelChoice},
					state.SetInteractionMode{Mode: state.InteractionModeNAV},
					interaction.FocusKeyAction{Key: "model"},
				}
			},
		})
	}
	if len(items) > 0 {
		return views.RenderFilterablePickerDisclosure(ctx, modelRow, pickerState, setPickerState, items, views.FilterablePickerConfig{
			KeyPrefix:      "create-model-option",
			BuildOptionRow: views.ChoicePickerOptionRow(false),
			WindowSize:     6,
			FindLabel:      "find",
			OnNoMatchFocus: func() []update.Action { return []update.Action{interaction.FocusKeyAction{Key: "model"}} },
			OnCancel: func() []update.Action {
				setModelPickerOpen(false)
				return []update.Action{
					state.SetInteractionMode{Mode: state.InteractionModeNAV},
					interaction.FocusKeyAction{Key: "model"},
				}
			},
		})
	}
	if model.CreateDraftModelError != "" {
		return toolkitviews.NewAnchoredDisclosure(modelRow, views.DisclosureNoteRows(model.CreateDraftModelError)...)
	}
	return toolkitviews.NewAnchoredDisclosure(modelRow, views.RowStatic("", "loading models…"))
}

func workspaceSection(ctx *view.Context[state.Model]) view.ViewSpec[state.Model] {
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
		view.Named[state.Model]("run_on", view.Build[state.Model](BuildRunOnWorkspaceRow)),
		view.Named[state.Model]("providers", view.Build[state.Model](BuildProvidersWorkspacePanel)),
	)
}

func firstRunProviderChoiceRow(label string, onActivate func() []update.Action) view.ViewSpec[state.Model] {
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
