package routing

import (
	"os"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func buildAddModelCredentialRow(model state.Model, endpointName string, draft state.ProviderConfigSnapshot, panel addModelPanelState) retained.ViewSpec[state.Model] {
	summary := addModelCredentialSummary(model, draft)
	row := views.RowActionWithCancel("credential", selectors.EmptyOr(summary, "not set"), "change", func() []update.Action {
		next := panel.credentialUI
		next.SourcePickerOpen = !next.SourcePickerOpen
		panel.setCredentialUI(next)
		return nil
	}, nil)
	if !panel.credentialUI.SourcePickerOpen {
		return row
	}
	options := credentialOptionRows(credentialSource(draft.CredentialRef), func(choice string) []update.Action {
		next := applyAddModelCredentialSourceChoice(draft, choice)
		panel.setDraft(next)
		nextUI := closeAddModelCredentialUIState(panel.credentialUI)
		if strings.EqualFold(strings.TrimSpace(choice), "file") { // swobu:io-string source=boundary
			currentPath := credentialFilePath(next.CredentialRef)
			nextUI.FileBrowse = initialCredentialFileBrowseState(currentPath)
			nextUI.FilePicker = views.DefaultFilterablePickerState()
		}
		panel.setCredentialUI(nextUI)
		variant := providercatalog.AuthVariant(strings.ToLower(strings.TrimSpace(choice))) // swobu:io-string source=boundary
		if providercatalog.IsInteractiveAuthVariant(variant) {
			next.CredentialRef = string(variant)
			// Browser flow starts only when operator activates "sign in".
			if strings.EqualFold(string(variant), "chatgpt_login") {
				return []update.Action{state.ResetAddModelAuthUIRequested{}}
			}
			// Device-code flow starts immediately to generate link+code.
			return startAuthActionsForAddModel(endpointName, next)
		}
		return nil
	}, func() []update.Action {
		next := panel.credentialUI
		next.SourcePickerOpen = false
		panel.setCredentialUI(next)
		return nil
	}, strings.TrimSpace(draft.ProviderSpec), false) // swobu:io-string source=boundary
	return toolkitviews.NewAnchoredDisclosure(row, options...)
}

func addModelCredentialSummary(model state.Model, draft state.ProviderConfigSnapshot) string {
	resolvedRef := strings.TrimSpace(effectiveAddModelCredentialRef(model, draft))                              // swobu:io-string source=boundary
	draftRef := strings.TrimSpace(draft.CredentialRef)                                                          // swobu:io-string source=boundary
	providerSpec := strings.TrimSpace(draft.ProviderSpec)                                                       // swobu:io-string source=boundary
	draftVariant := providercatalog.AuthVariant(strings.ToLower(strings.TrimSpace(credentialSource(draftRef)))) // swobu:io-string source=boundary
	if providercatalog.SupportsAuthVariant(providerSpec, draftVariant) &&
		providercatalog.IsInteractiveAuthVariant(draftVariant) &&
		resolvedRef != "" &&
		!strings.EqualFold(resolvedRef, draftRef) {
		return "signed in"
	}
	source := strings.TrimSpace(credentialSource(resolvedRef)) // swobu:io-string source=boundary
	if source == "" {
		return "missing"
	}
	if strings.EqualFold(providerSpec, "bedrock") && isBedrockAWSProfileCredentialRef(resolvedRef) {
		return "AWS profile"
	}
	variant := providercatalog.AuthVariant(strings.ToLower(source)) // swobu:io-string source=boundary
	if providercatalog.SupportsAuthVariant(providerSpec, variant) && providercatalog.IsInteractiveAuthVariant(variant) {
		if resolvedRef != "" && !strings.EqualFold(resolvedRef, string(variant)) {
			return "signed in"
		}
		return authVariantDisplayLabel(variant)
	}
	if isResolvedInteractiveCredential(providerSpec, resolvedRef) {
		return "signed in"
	}
	if strings.EqualFold(source, "env") {
		if strings.EqualFold(providerSpec, "bedrock") {
			return "Bedrock API key"
		}
		key := strings.TrimSpace(envCredentialKey(resolvedRef)) // swobu:io-string source=boundary
		if key == "" {
			key = strings.TrimSpace(providercatalog.DefaultEnvKeyForSpec(providerSpec)) // swobu:io-string source=boundary
		}
		if key != "" {
			if _, ok := os.LookupEnv(key); !ok {
				return "env var missing"
			}
		}
		return "env var"
	}
	if strings.EqualFold(source, "file") {
		path := strings.TrimSpace(credentialFilePath(resolvedRef)) // swobu:io-string source=boundary
		if path == "" {
			return "file missing"
		}
		if _, err := os.Stat(path); err != nil {
			return "file missing"
		}
		return "file"
	}
	if providercatalog.SupportsAuthVariant(providerSpec, variant) {
		return authVariantDisplayLabel(variant)
	}
	return selectors.CredentialSummaryFromProviderConfig(&draft)
}

func startAuthActionsForAddModel(endpointName string, providerConfig state.ProviderConfigSnapshot) []update.Action {
	return startAuthActionsForFlow(endpointName, providerConfig, stateModel.AuthScopeEndpointProvider)
}

func startAuthActionsForCreateDraft(providerConfig state.ProviderConfigSnapshot) []update.Action {
	return startAuthActionsForFlow("", providerConfig, stateModel.AuthScopeCreateDraft)
}

func startAuthActionsForFlow(endpointName string, providerConfig state.ProviderConfigSnapshot, authScope string) []update.Action {
	ownerKey := stateModel.EndpointProviderAuthOwnerKey(strings.TrimSpace(endpointName), strings.TrimSpace(providerConfig.Ref)).String() // swobu:io-string source=boundary
	if strings.TrimSpace(authScope) == stateModel.AuthScopeCreateDraft {                                                                 // swobu:io-string source=boundary
		ownerKey = stateModel.CreateDraftAuthOwnerKey(strings.TrimSpace(providerConfig.Ref)).String() // swobu:io-string source=boundary
	} else {
		ownerKey = stateModel.AddModelDraftAuthOwnerKey(strings.TrimSpace(endpointName), strings.TrimSpace(providerConfig.Ref)).String() // swobu:io-string source=boundary
	}
	return []update.Action{
		state.StartProviderAuthSessionRequested{
			EndpointName:   strings.TrimSpace(endpointName), // swobu:io-string source=boundary
			ProviderConfig: providerConfig,
			OwnerKey:       ownerKey,
			AuthScope:      strings.TrimSpace(authScope), // swobu:io-string source=boundary
		},
	}
}

func buildAddModelEnvKeyRow(_ *retained.Context[state.Model], model state.Model, draft state.ProviderConfigSnapshot, panel addModelPanelState) retained.ViewSpec[state.Model] {
	return retained.Build(func(childCtx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		current := strings.TrimSpace(envCredentialKey(draft.CredentialRef))                   // swobu:io-string source=boundary
		summary, editorValue := envKeySummary(strings.TrimSpace(draft.ProviderSpec), current) // swobu:io-string source=boundary
		return backendURLEditorRow(
			childCtx,
			"env key",
			summary,
			editorValue,
			"env variable",
			func(value string) []update.Action {
				next := draft
				next.CredentialRef = encodeCredentialEnvRef(value)
				next.ModelID = ""
				panel.setDraft(next)
				return []update.Action{
					state.LoadRoutingModelCatalogRequestedAction{
						Scope:         state.RoutingModelCatalogScopeAddModelDraft,
						ProviderSpec:  strings.TrimSpace(next.ProviderSpec),  // swobu:io-string source=boundary
						BaseURL:       strings.TrimSpace(next.BaseURL),       // swobu:io-string source=boundary
						CredentialRef: strings.TrimSpace(next.CredentialRef), // swobu:io-string source=boundary // swobu:io-string source=boundary
					},
					state.SetInteractionMode{Mode: state.InteractionModeManageList},
					interaction.FocusKeyAction{Key: "add-model/env-key"},
				}
			},
		)
	})
}

func buildAddModelKeychainKeyNameRow(_ *retained.Context[state.Model], model state.Model, draft state.ProviderConfigSnapshot, panel addModelPanelState) retained.ViewSpec[state.Model] {
	return retained.Build(func(childCtx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		currentName := keychainCredentialName(draft.CredentialRef)
		effectiveName := keychainEffectiveName(draft.ProviderSpec, currentName)
		keyNameRow := backendURLEditorRow(
			childCtx,
			"key slot",
			effectiveName,
			effectiveName,
			"provider/default",
			func(value string) []update.Action {
				next := draft
				next.CredentialRef = encodeCredentialKeychainRef(value)
				next.ModelID = ""
				panel.setDraft(next)
				return []update.Action{
					state.SetInteractionMode{Mode: state.InteractionModeManageList},
					interaction.FocusKeyAction{Key: "add-model/keychain-key-name"},
				}
			},
		)
		keyValueRow := backendURLEditorRow(
			childCtx,
			"key value",
			keychainValueSummary(model, draft.ProviderSpec, effectiveName),
			"",
			"paste key value",
			func(value string) []update.Action {
				return routingStoreKeychainCredentialActions(strings.TrimSpace(draft.ProviderSpec), effectiveName, strings.TrimSpace(value), "add-model/keychain") // swobu:io-string source=boundary
			},
		)
		return retained.VStack(childCtx, keyNameRow, keyValueRow)
	})
}

func buildAddModelCredentialFileRow(ctx *retained.Context[state.Model], draft state.ProviderConfigSnapshot, panel addModelPanelState) retained.ViewSpec[state.Model] {
	closeMode := state.InteractionModeManageList
	currentPath := credentialFilePath(draft.CredentialRef)
	ui := panel.credentialUI
	parent := credentialFileRow(currentPath, func() []update.Action {
		nextOpen := !ui.FilePickerOpen
		nextUI := ui
		nextUI.FilePickerOpen = nextOpen
		actions := make([]update.Action, 0, 2)
		if nextOpen {
			nextUI.FileBrowse = initialCredentialFileBrowseState(currentPath)
			nextUI.FilePicker = views.DefaultFilterablePickerState()
			actions = append(actions, interaction.FocusKeyAction{Key: views.FilterablePickerFocusKey("credential-file-option", 0)})
		}
		panel.setCredentialUI(nextUI)
		if nextOpen {
			actions = append(actions, state.SetInteractionMode{Mode: state.InteractionModePickOne})
		} else {
			actions = append(actions, state.SetInteractionMode{Mode: closeMode})
		}
		return actions
	}, func() []update.Action {
		if ui.FilePickerOpen {
			nextUI := ui
			nextUI.FilePickerOpen = false
			panel.setCredentialUI(nextUI)
			return []update.Action{
				state.SetInteractionMode{Mode: closeMode},
				interaction.FocusKeyAction{Key: "add-model/credential-file"},
			}
		}
		return nil
	})
	if !ui.FilePickerOpen {
		return parent
	}
	items, err := addModelCredentialFilePickerItems(ui, panel.setCredentialUI, currentPath, func(path string) []update.Action {
		next := applyAddModelCredentialFilePathChoice(draft, path)
		panel.setDraft(next)
		nextUI := panel.credentialUI
		nextUI.FilePickerOpen = false
		panel.setCredentialUI(nextUI)
		return []update.Action{
			state.SetInteractionMode{Mode: closeMode},
			interaction.FocusKeyAction{Key: "add-model/credential-file"},
		}
	})
	if err != nil {
		return toolkitviews.NewAnchoredDisclosure(parent, views.DisclosureNoteRows(err.Error())...)
	}
	return views.RenderFilterablePickerDisclosure(ctx, parent, ui.FilePicker, func(nextPicker views.FilterablePickerState) {
		nextUI := panel.credentialUI
		nextUI.FilePicker = nextPicker
		panel.setCredentialUI(nextUI)
	}, items, views.FilterablePickerConfig{
		KeyPrefix:      "credential-file-option",
		BuildOptionRow: views.ChoicePickerOptionRow(false),
		WindowSize:     6,
		FindLabel:      "find",
		NoMatchesLabel: "no files",
		HeaderRows: []retained.ViewSpec[state.Model]{
			views.RowStatic("path", credentialFileBrowserPath(ui.FileBrowse.Dir)),
		},
		OnNoMatchFocus: func() []update.Action {
			return []update.Action{interaction.FocusKeyAction{Key: "add-model/credential-file"}}
		},
		OnCancel: func() []update.Action {
			nextUI := panel.credentialUI
			nextUI.FilePickerOpen = false
			panel.setCredentialUI(nextUI)
			return []update.Action{
				state.SetInteractionMode{Mode: closeMode},
				interaction.FocusKeyAction{Key: "add-model/credential-file"},
			}
		},
	})
}

func addModelCredentialFilePickerItems(
	ui addModelCredentialUIState,
	setCredentialUI func(addModelCredentialUIState),
	currentPath string,
	onChooseFile func(string) []update.Action,
) ([]views.FilterablePickerItem, error) {
	return credentialFilePickerItems(ui.FileBrowse, func(nextBrowse credentialFileBrowseState) {
		nextUI := ui
		nextUI.FileBrowse = nextBrowse
		nextUI.FilePicker = views.DefaultFilterablePickerState()
		setCredentialUI(nextUI)
	}, func() []update.Action {
		return []update.Action{
			interaction.FocusKeyAction{Key: views.FilterablePickerFocusKey("credential-file-option", 0)},
		}
	}, currentPath, onChooseFile)
}

func applyAddModelCredentialSourceChoice(draft state.ProviderConfigSnapshot, choice string) state.ProviderConfigSnapshot {
	next := draft
	ref := strings.TrimSpace(choice) // swobu:io-string source=boundary
	if strings.EqualFold(ref, "env") {
		ref = encodeCredentialEnvRef(providercatalog.DefaultEnvKeyForSpec(strings.TrimSpace(next.ProviderSpec))) // swobu:io-string source=boundary
	}
	if strings.EqualFold(ref, "file") {
		ref = encodeCredentialFileRef("")
	}
	next.CredentialRef = ref
	next.ModelID = ""
	return next
}

func applyAddModelCredentialFilePathChoice(draft state.ProviderConfigSnapshot, path string) state.ProviderConfigSnapshot {
	next := draft
	next.CredentialRef = encodeCredentialFileRef(path)
	next.ModelID = ""
	return next
}

func buildAddModelCreateRow(model state.Model, snapshot *state.EndpointSnapshot, draft state.ProviderConfigSnapshot, panel addModelPanelState) retained.ViewSpec[state.Model] {
	resolvedDraft := draft
	resolvedDraft.CredentialRef = effectiveAddModelCredentialRef(model, draft)
	ready := addModelCreateReady(resolvedDraft)
	if !ready {
		return nil
	}
	createDraft := resolvedDraft
	return retained.Named[state.Model]("add-model/create", views.RowAction("create model", "", "create", func() []update.Action {
		panel.setOpen(false)
		panel.setCredentialUI(closeAddModelCredentialUIState(panel.credentialUI))
		return routingAddProviderConfigActions(strings.TrimSpace(snapshot.Name), createDraft, "add-model/create") // swobu:io-string source=boundary
	}))
}

func effectiveAddModelCredentialRef(model state.Model, draft state.ProviderConfigSnapshot) string {
	ref := strings.TrimSpace(draft.CredentialRef)         // swobu:io-string source=boundary
	providerSpec := strings.TrimSpace(draft.ProviderSpec) // swobu:io-string source=boundary
	interactiveVariants := make(map[string]struct{}, 2)
	for _, variant := range providercatalog.SupportedAuthVariantsForSpec(providerSpec) {
		if providercatalog.IsInteractiveAuthVariant(variant) {
			interactiveVariants[strings.ToLower(strings.TrimSpace(string(variant)))] = struct{}{} // swobu:io-string source=boundary
		}
	}
	if len(interactiveVariants) == 0 {
		return ref
	}
	if ref != "" {
		if _, ok := interactiveVariants[strings.ToLower(ref)]; !ok { // swobu:io-string source=boundary
			return ref
		}
	}
	if model.AddModelDraftProviderSpec != providerSpec {
		return ref
	}
	if model.AddModelDraftBaseURL != draft.BaseURL {
		return ref
	}
	resolved := model.AddModelDraftCredentialRef
	if resolved == "" {
		return ref
	}
	return resolved
}

func buildAddModelModelChoiceRow(ctx *retained.Context[state.Model], model state.Model, panel addModelPanelState) retained.ViewSpec[state.Model] {
	return buildDraftModelChoiceRow(ctx, draftModelRowSpec{
		Binding: addDraftModelBinding{
			model:    model,
			draft:    panel.draft,
			setDraft: panel.setDraft,
		},
		PickerOpen:     panel.modelPickerOpen,
		SetPickerOpen:  panel.setModelPickerOpen,
		PickerState:    panel.modelPicker,
		SetPickerState: panel.setModelPicker,
		KeyPrefix:      "add-model-option",
		FocusKey:       "add-model/model",
	})
}
