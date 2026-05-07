package routing

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/view"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
)

type addModelPanelState struct {
	open                  bool
	setOpen               func(bool)
	draft                 state.ProviderConfigSnapshot
	setDraft              func(state.ProviderConfigSnapshot)
	providerPicker        views.FilterablePickerState
	setProviderPicker     func(views.FilterablePickerState)
	providerPickerOpen    bool
	setProviderPickerOpen func(bool)
	modelPicker           views.FilterablePickerState
	setModelPicker        func(views.FilterablePickerState)
	modelPickerOpen       bool
	setModelPickerOpen    func(bool)
	credentialUI          addModelCredentialUIState
	setCredentialUI       func(addModelCredentialUIState)
}

type addModelCredentialUIState struct {
	SourcePickerOpen bool
	FilePickerOpen   bool
	FileBrowse       credentialFileBrowseState
	FilePicker       views.FilterablePickerState
}

func defaultAddModelCredentialUIState(path string) addModelCredentialUIState {
	return addModelCredentialUIState{
		SourcePickerOpen: false,
		FilePickerOpen:   false,
		FileBrowse:       initialCredentialFileBrowseState(path),
		FilePicker:       views.DefaultFilterablePickerState(),
	}
}

func closeAddModelCredentialUIState(ui addModelCredentialUIState) addModelCredentialUIState {
	ui.SourcePickerOpen = false
	ui.FilePickerOpen = false
	return ui
}

func buildProvidersWorkspaceConfiguredPanel(ctx *view.Context[state.Model], model state.Model, snapshot *state.EndpointSnapshot) view.ViewSpec[state.Model] {
	expandedRef, setExpandedRef := view.UseState(ctx, func() string { return strings.TrimSpace(snapshot.SelectedProviderConfigRef) })
	open, setOpen := view.UseState(ctx, func() bool { return false })
	addOpen, setAddOpen := view.UseState(ctx, func() bool { return false })
	addDraft, setAddDraft := view.UseState(ctx, func() state.ProviderConfigSnapshot {
		return state.ProviderConfigSnapshot{Ref: nextProviderRef(snapshot), ProtocolKind: defaultProtocolKindForProvider("")}
	})
	addProviderPicker, setAddProviderPicker := view.UseState(ctx, func() views.FilterablePickerState { return views.DefaultFilterablePickerState() })
	addProviderPickerOpen, setAddProviderPickerOpen := view.UseState(ctx, func() bool { return false })
	addModelPicker, setAddModelPicker := view.UseState(ctx, func() views.FilterablePickerState { return views.DefaultFilterablePickerState() })
	addModelPickerOpen, setAddModelPickerOpen := view.UseState(ctx, func() bool { return false })
	addCredentialUI, setAddCredentialUI := view.UseState(ctx, func() addModelCredentialUIState {
		return defaultAddModelCredentialUIState("")
	})

	onClose := func() []update.Action {
		if !open {
			return nil
		}
		setOpen(false)
		setExpandedRef("")
		return []update.Action{
			state.SetInteractionMode{Mode: state.InteractionModeNAV},
			interaction.FocusKeyAction{Key: "providers"},
		}
	}
	rows := buildWorkspaceProviderRows(model, snapshot, expandedRef, setExpandedRef, onClose)
	addRows := buildWorkspaceAddModelRows(ctx, model, snapshot, addModelPanelState{
		open:                  addOpen,
		setOpen:               setAddOpen,
		draft:                 addDraft,
		setDraft:              setAddDraft,
		providerPicker:        addProviderPicker,
		setProviderPicker:     setAddProviderPicker,
		providerPickerOpen:    addProviderPickerOpen,
		setProviderPickerOpen: setAddProviderPickerOpen,
		modelPicker:           addModelPicker,
		setModelPicker:        setAddModelPicker,
		modelPickerOpen:       addModelPickerOpen,
		setModelPickerOpen:    setAddModelPickerOpen,
		credentialUI:          addCredentialUI,
		setCredentialUI:       setAddCredentialUI,
	})
	rows = append(rows, addRows...)
	parent := view.Named[state.Model]("providers", views.RowManageWithHooks(
		views.RowProviders,
		workspaceModelsCountSummary(snapshot),
		func() []update.Action {
			nextOpen := !open
			setOpen(nextOpen)
			if !nextOpen {
				setExpandedRef("")
				return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeNAV}}
			}
			return []update.Action{
				state.SetInteractionMode{Mode: state.InteractionModeManageList},
				interaction.FocusKeyAction{Key: "provider-summary/" + providerRowKey(snapshot.SelectedProviderConfigRef)},
			}
		},
		onClose,
		views.FocusAffordance("manage", false),
	))
	if !open {
		return parent
	}
	if len(rows) == 0 {
		return parent
	}
	return views.EscClosableDisclosure(parent, true, onClose, rows...)
}

func buildWorkspaceProviderRows(
	model state.Model,
	snapshot *state.EndpointSnapshot,
	expandedRef string,
	setExpandedRef func(string),
	_ func() []update.Action,
) []view.ViewSpec[state.Model] {
	rows := make([]view.ViewSpec[state.Model], 0, len(snapshot.ProviderConfigs))
	endpointName := strings.TrimSpace(snapshot.Name)
	selectedRef := strings.TrimSpace(snapshot.SelectedProviderConfigRef)
	for _, pc := range snapshot.ProviderConfigs {
		ref := strings.TrimSpace(pc.Ref)
		isExpanded := strings.TrimSpace(expandedRef) == ref
		choice := ref
		closeExpanded := func() []update.Action {
			if !isExpanded {
				return nil
			}
			setExpandedRef("")
			return []update.Action{
				state.SetInteractionMode{Mode: state.InteractionModeManageList},
				interaction.FocusKeyAction{Key: "provider-summary/" + providerRowKey(ref)},
			}
		}
		parent := view.Named[state.Model]("provider-summary/"+providerRowKey(ref), newProviderSummaryRow(
			pc,
			ref == selectedRef,
			isExpanded,
			func() []update.Action {
				if isExpanded {
					setExpandedRef("")
				} else {
					setExpandedRef(choice)
				}
				return nil
			},
			closeExpanded,
		))
		if isExpanded {
			catalog := catalogEntryForProvider(model, endpointName, ref)
			children := createProviderPropertyRows(endpointName, catalog, &pc, false)
			parent = views.EscClosableDisclosure(parent, true, closeExpanded, children...)
		}
		rows = append(rows, parent)
	}
	return rows
}

func buildWorkspaceAddModelRows(
	ctx *view.Context[state.Model],
	model state.Model,
	snapshot *state.EndpointSnapshot,
	panel addModelPanelState,
) []view.ViewSpec[state.Model] {
	label := "add model"
	verb := "create"
	if len(snapshot.ProviderConfigs) == 0 {
		label = "add first model"
		verb = "start"
	}
	if panel.open {
		verb = "close"
	}
	addRow := views.RowActionWithCancel(label, "", verb, func() []update.Action {
		return toggleAddModelLane(snapshot, panel)
	}, func() []update.Action {
		if panel.open {
			panel.setOpen(false)
		}
		panel.setProviderPickerOpen(false)
		panel.setModelPickerOpen(false)
		return nil
	})
	parent := view.Named[state.Model]("add-model", addRow)
	if !panel.open {
		return []view.ViewSpec[state.Model]{parent}
	}

	detailRows := buildWorkspaceAddModelDetailRows(ctx, model, snapshot, panel)
	closeAddModel := func() []update.Action {
		if !panel.open {
			return nil
		}
		panel.setOpen(false)
		panel.setProviderPickerOpen(false)
		panel.setModelPickerOpen(false)
		panel.setCredentialUI(closeAddModelCredentialUIState(panel.credentialUI))
		return []update.Action{
			state.SetInteractionMode{Mode: state.InteractionModeManageList},
			interaction.FocusKeyAction{Key: "add-model"},
		}
	}
	return []view.ViewSpec[state.Model]{
		views.EscClosableDisclosure(parent, true, closeAddModel, detailRows...),
	}
}

func toggleAddModelLane(snapshot *state.EndpointSnapshot, panel addModelPanelState) []update.Action {
	if panel.open {
		panel.setOpen(false)
		return nil
	}
	draft := state.ProviderConfigSnapshot{Ref: nextProviderRef(snapshot), ProtocolKind: defaultProtocolKindForProvider("")}
	panel.setDraft(draft)
	panel.setOpen(true)
	panel.setProviderPickerOpen(false)
	panel.setModelPickerOpen(false)
	panel.setCredentialUI(defaultAddModelCredentialUIState(""))
	views.ResetFilterablePickerState(panel.setProviderPicker)
	views.ResetFilterablePickerState(panel.setModelPicker)
	return nil
}

func buildWorkspaceAddModelDetailRows(
	ctx *view.Context[state.Model],
	model state.Model,
	snapshot *state.EndpointSnapshot,
	panel addModelPanelState,
) []view.ViewSpec[state.Model] {
	draft := panel.draft
	if strings.TrimSpace(draft.Ref) == "" {
		draft.Ref = nextProviderRef(snapshot)
	}
	rows := []view.ViewSpec[state.Model]{
		view.Named[state.Model]("add-model/provider", buildAddModelProviderRow(ctx, model, draft, panel)),
		view.Named[state.Model]("add-model/credentials", buildAddModelCredentialRow(draft, panel)),
	}
	rows = appendWorkspaceAddModelCredentialRows(ctx, model, rows, draft, panel)
	rows = append(rows,
		view.Named[state.Model]("add-model/model", buildAddModelModelChoiceRow(ctx, panel)),
		view.Named[state.Model]("add-model/id", backendURLEditorRow(ctx, views.RowTargetAlias, selectors.EmptyOr(strings.TrimSpace(draft.TargetAlias), "not set"), strings.TrimSpace(draft.TargetAlias), "fast", func(value string) []update.Action {
			next := draft
			next.TargetAlias = strings.TrimSpace(strings.ToLower(value))
			panel.setDraft(next)
			return nil
		})),
	)
	if strings.EqualFold(strings.TrimSpace(draft.ProviderSpec), "custom") {
		rows = append(rows, view.Named[state.Model]("add-model/backend-url", backendURLEditorRow(ctx, views.RowBackendURL, selectors.EmptyOr(strings.TrimSpace(draft.BaseURL), "missing"), strings.TrimSpace(draft.BaseURL), "https://host/v1", func(value string) []update.Action {
			next := draft
			next.BaseURL = strings.TrimSpace(value)
			panel.setDraft(next)
			return nil
		})))
	}
	rows = append(rows, buildAddModelCreateRow(snapshot, draft, panel))
	return rows
}

func appendWorkspaceAddModelCredentialRows(
	ctx *view.Context[state.Model],
	model state.Model,
	rows []view.ViewSpec[state.Model],
	draft state.ProviderConfigSnapshot,
	panel addModelPanelState,
) []view.ViewSpec[state.Model] {
	source := credentialSource(strings.TrimSpace(draft.CredentialRef))
	if strings.EqualFold(source, "env") {
		rows = append(rows, view.Named[state.Model]("add-model/env-key", buildAddModelEnvKeyRow(ctx, model, draft, panel)))
	}
	if strings.EqualFold(source, "keychain") {
		rows = append(rows, view.Named[state.Model]("add-model/keychain-key-name", buildAddModelKeychainKeyNameRow(ctx, model, draft, panel)))
	}
	if strings.EqualFold(source, "file") {
		rows = append(rows, view.Named[state.Model]("add-model/credential-file", buildAddModelCredentialFileRow(ctx, draft, panel)))
	}
	return rows
}

func buildAddModelProviderRow(
	ctx *view.Context[state.Model],
	model state.Model,
	draft state.ProviderConfigSnapshot,
	panel addModelPanelState,
) view.ViewSpec[state.Model] {
	providerRow := views.RowChoiceWithCancel("provider", selectors.EmptyOr(providercatalog.DisplayName(strings.TrimSpace(draft.ProviderSpec)), "choose a provider"), func() []update.Action {
		panel.setProviderPickerOpen(true)
		views.ResetFilterablePickerState(panel.setProviderPicker)
		return []update.Action{state.SetInteractionMode{Mode: state.InteractionModePickOne}}
	}, func() []update.Action {
		if !panel.providerPickerOpen {
			return nil
		}
		panel.setProviderPickerOpen(false)
		return []update.Action{
			state.SetInteractionMode{Mode: state.InteractionModeManageList},
			interaction.FocusKeyAction{Key: "add-model/provider-row"},
		}
	})
	providerRowNamed := view.Named[state.Model]("add-model/provider-row", providerRow)
	if !panel.providerPickerOpen {
		return providerRowNamed
	}
	items := buildAddModelProviderItems(model, draft, panel)
	return views.RenderFilterablePickerDisclosure(ctx, providerRowNamed, panel.providerPicker, panel.setProviderPicker, items, views.FilterablePickerConfig{
		KeyPrefix:      "add-provider-option",
		BuildOptionRow: views.ChoicePickerOptionRow(true),
		WindowSize:     6,
		FindLabel:      "find",
		ShowSelected:   true,
		OnNoMatchFocus: func() []update.Action {
			return []update.Action{interaction.FocusKeyAction{Key: "add-model/provider-row"}}
		},
		OnCancel: func() []update.Action {
			panel.setProviderPickerOpen(false)
			return []update.Action{
				state.SetInteractionMode{Mode: state.InteractionModeManageList},
				interaction.FocusKeyAction{Key: "add-model/provider-row"},
			}
		},
	})
}

func buildAddModelProviderItems(model state.Model, draft state.ProviderConfigSnapshot, panel addModelPanelState) []views.FilterablePickerItem {
	items := createProviderSpecItems(model, nil)
	for i := range items {
		item := items[i]
		spec := providerSpecFromSearch(item.Search)
		items[i].OnChoose = func() []update.Action {
			next := state.ProviderConfigForSpec(spec, draft)
			next.Ref = draft.Ref
			next.ModelID = ""
			next.TargetAlias = ""
			panel.setDraft(next)
			panel.setProviderPickerOpen(false)
			panel.setModelPickerOpen(false)
			panel.setCredentialUI(closeAddModelCredentialUIState(panel.credentialUI))
			focusKey := "add-model/model"
			if providerCredentialSelectionRequired(strings.TrimSpace(next.ProviderSpec), strings.TrimSpace(next.BaseURL), strings.TrimSpace(next.CredentialRef)) {
				focusKey = "add-model/credentials"
			}
			return []update.Action{
				state.SetInteractionMode{Mode: state.InteractionModeManageList},
				interaction.FocusKeyAction{Key: focusKey},
			}
		}
	}
	return items
}

func providerSpecFromSearch(search string) string {
	spec := strings.TrimSpace(strings.ToLower(search))
	if strings.Contains(spec, " ") {
		spec = strings.Split(spec, " ")[0]
	}
	return spec
}

func workspaceModelsCountSummary(snapshot *state.EndpointSnapshot) string {
	if snapshot == nil {
		return "not configured"
	}
	count := len(snapshot.ProviderConfigs)
	if count == 1 {
		return "1 configured"
	}
	return fmt.Sprintf("%d configured", count)
}

func buildAddModelCredentialRow(draft state.ProviderConfigSnapshot, panel addModelPanelState) view.ViewSpec[state.Model] {
	summary := selectors.CredentialSummaryFromProviderConfig(&draft)
	row := views.RowChoiceWithCancel("credentials", selectors.EmptyOr(summary, "not set"), func() []update.Action {
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
		if strings.EqualFold(strings.TrimSpace(choice), "file") {
			currentPath := credentialFilePath(next.CredentialRef)
			nextUI.FileBrowse = initialCredentialFileBrowseState(currentPath)
			nextUI.FilePicker = views.DefaultFilterablePickerState()
		}
		panel.setCredentialUI(nextUI)
		return nil
	}, func() []update.Action {
		next := panel.credentialUI
		next.SourcePickerOpen = false
		panel.setCredentialUI(next)
		return nil
	})
	return toolkitviews.NewAnchoredDisclosure(row, options...)
}

func buildAddModelEnvKeyRow(_ *view.Context[state.Model], model state.Model, draft state.ProviderConfigSnapshot, panel addModelPanelState) view.ViewSpec[state.Model] {
	return view.Build(func(childCtx *view.Context[state.Model]) view.ViewSpec[state.Model] {
		current := strings.TrimSpace(envCredentialKey(draft.CredentialRef))
		summary, editorValue := envKeySummary(strings.TrimSpace(draft.ProviderSpec), current)
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
					state.LoadAddModelDraftModelCatalogRequested{
						ProviderSpec:  strings.TrimSpace(next.ProviderSpec),
						BaseURL:       strings.TrimSpace(next.BaseURL),
						CredentialRef: strings.TrimSpace(next.CredentialRef),
						ProtocolKind:  defaultProtocolKindForProvider(strings.TrimSpace(next.ProviderSpec)),
					},
					state.SetInteractionMode{Mode: state.InteractionModeManageList},
					interaction.FocusKeyAction{Key: "add-model/env-key"},
				}
			},
		)
	})
}

func buildAddModelKeychainKeyNameRow(_ *view.Context[state.Model], model state.Model, draft state.ProviderConfigSnapshot, panel addModelPanelState) view.ViewSpec[state.Model] {
	return view.Build(func(childCtx *view.Context[state.Model]) view.ViewSpec[state.Model] {
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
				return []update.Action{state.StoreKeychainCredentialRequested{
					ProviderSpec: strings.TrimSpace(draft.ProviderSpec),
					KeyName:      effectiveName,
					Secret:       strings.TrimSpace(value),
				}}
			},
		)
		return view.VStack(childCtx, keyNameRow, keyValueRow)
	})
}

func buildAddModelCredentialFileRow(ctx *view.Context[state.Model], draft state.ProviderConfigSnapshot, panel addModelPanelState) view.ViewSpec[state.Model] {
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
		HeaderRows: []view.ViewSpec[state.Model]{
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
	ref := strings.TrimSpace(choice)
	if strings.EqualFold(ref, "env") {
		ref = encodeCredentialEnvRef(providercatalog.DefaultEnvKeyForSpec(strings.TrimSpace(next.ProviderSpec)))
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

func buildAddModelCreateRow(snapshot *state.EndpointSnapshot, draft state.ProviderConfigSnapshot, panel addModelPanelState) view.ViewSpec[state.Model] {
	ready := strings.TrimSpace(draft.ProviderSpec) != "" &&
		strings.TrimSpace(draft.ModelID) != "" &&
		(!providerCredentialSelectionRequired(draft.ProviderSpec, draft.BaseURL, draft.CredentialRef) || strings.TrimSpace(draft.CredentialRef) != "") &&
		!isEmptyFileCredentialRef(draft.CredentialRef) &&
		(!strings.EqualFold(strings.TrimSpace(draft.ProviderSpec), "custom") || strings.TrimSpace(draft.BaseURL) != "")
	if !ready {
		return view.Named[state.Model]("add-model/create", views.RowStatic("create model", "not ready"))
	}
	createDraft := draft
	return view.Named[state.Model]("add-model/create", views.RowAction("create model", "", "create", func() []update.Action {
		panel.setOpen(false)
		panel.setCredentialUI(closeAddModelCredentialUIState(panel.credentialUI))
		return []update.Action{
			state.RoutingSaveStartedAction{},
			state.AddProviderConfigRequested{
				EndpointName:   strings.TrimSpace(snapshot.Name),
				ProviderConfig: createDraft,
			},
		}
	}))
}

func isEmptyFileCredentialRef(ref string) bool {
	trimmed := strings.TrimSpace(ref)
	if strings.EqualFold(trimmed, "file") {
		return true
	}
	return strings.HasPrefix(strings.ToLower(trimmed), fileCredentialRefPrefix) && strings.TrimSpace(credentialFilePath(trimmed)) == ""
}

func buildAddModelModelChoiceRow(ctx *view.Context[state.Model], panel addModelPanelState) view.ViewSpec[state.Model] {
	return buildDraftModelChoiceRow(ctx, draftModelRowSpec{
		Binding: addDraftModelBinding{
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
