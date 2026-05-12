package routing

import (
	"fmt"
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

func buildProvidersWorkspaceConfiguredPanel(ctx *retained.Context[state.Model], model state.Model, snapshot *state.EndpointSnapshot) retained.ViewSpec[state.Model] {
	expandedRef, setExpandedRef := retained.UseState(ctx, func() string { return "" })
	open, setOpen := retained.UseState(ctx, func() bool { return false })
	addOpen, setAddOpen := retained.UseState(ctx, func() bool { return false })
	addDraft, setAddDraft := retained.UseState(ctx, func() state.ProviderConfigSnapshot {
		return state.ProviderConfigSnapshot{Ref: nextProviderRef(snapshot), ProtocolKind: defaultProtocolKindForProvider("")}
	})
	addProviderPicker, setAddProviderPicker := retained.UseState(ctx, func() views.FilterablePickerState { return views.DefaultFilterablePickerState() })
	addProviderPickerOpen, setAddProviderPickerOpen := retained.UseState(ctx, func() bool { return false })
	addModelPicker, setAddModelPicker := retained.UseState(ctx, func() views.FilterablePickerState { return views.DefaultFilterablePickerState() })
	addModelPickerOpen, setAddModelPickerOpen := retained.UseState(ctx, func() bool { return false })
	addCredentialUI, setAddCredentialUI := retained.UseState(ctx, func() addModelCredentialUIState {
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
	parent := retained.Named[state.Model]("providers", views.RowManageWithHooks(
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
) []retained.ViewSpec[state.Model] {
	rows := make([]retained.ViewSpec[state.Model], 0, len(snapshot.ProviderConfigs))
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
		parent := retained.Named[state.Model]("provider-summary/"+providerRowKey(ref), newProviderSummaryRow(
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
			children := createProviderPropertyRows(endpointName, &pc, false, model)
			parent = views.EscClosableDisclosure(parent, true, closeExpanded, children...)
		}
		rows = append(rows, parent)
	}
	return rows
}

func buildWorkspaceAddModelRows(
	ctx *retained.Context[state.Model],
	model state.Model,
	snapshot *state.EndpointSnapshot,
	panel addModelPanelState,
) []retained.ViewSpec[state.Model] {
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
	parent := retained.Named[state.Model]("add-model", addRow)
	if !panel.open {
		return []retained.ViewSpec[state.Model]{parent}
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
	return []retained.ViewSpec[state.Model]{
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
	ctx *retained.Context[state.Model],
	model state.Model,
	snapshot *state.EndpointSnapshot,
	panel addModelPanelState,
) []retained.ViewSpec[state.Model] {
	draft := panel.draft
	if strings.TrimSpace(draft.Ref) == "" {
		draft.Ref = nextProviderRef(snapshot)
	}
	providerSpec := strings.TrimSpace(draft.ProviderSpec)
	rows := []retained.ViewSpec[state.Model]{
		retained.Named[state.Model]("add-model/provider", buildAddModelProviderRow(ctx, model, strings.TrimSpace(snapshot.Name), draft, panel)),
		retained.Named[state.Model]("add-model/credentials", buildAddModelCredentialRow(model, strings.TrimSpace(snapshot.Name), draft, panel)),
	}
	effectiveCredentialRef := effectiveAddModelCredentialRef(model, draft)
	authVariant := providercatalog.AuthVariant(strings.ToLower(strings.TrimSpace(credentialSource(strings.TrimSpace(draft.CredentialRef)))))
	authViewState := addModelChatGPTAuthViewNone
	if strings.EqualFold(providerSpec, "chatgpt") && providercatalog.IsInteractiveAuthVariant(authVariant) {
		authViewState = classifyAddModelChatGPTAuthViewState(model, strings.TrimSpace(snapshot.Name), draft, authVariant)
	}
	modelCatalogBlocked := providerModelCatalogLoadBlocked(
		providerSpec,
		strings.TrimSpace(draft.BaseURL),
		effectiveCredentialRef,
	)
	rows = appendWorkspaceAddModelCredentialRows(ctx, model, strings.TrimSpace(snapshot.Name), modelCatalogBlocked, rows, draft, panel)
	if providerSpec != "" && !modelCatalogBlocked {
		rows = append(rows, retained.Named[state.Model]("add-model/model", buildAddModelModelChoiceRow(ctx, model, panel)))
	} else if providerSpec != "" && modelCatalogBlocked &&
		!(strings.EqualFold(providerSpec, "chatgpt") && providercatalog.IsInteractiveAuthVariant(authVariant) && authViewState != addModelChatGPTAuthViewSignedIn) {
		rows = append(rows, retained.Named[state.Model]("add-model/model-blocked", views.RowStatic("model", "choose after auth")))
	}
	if providerSpec != "" && !modelCatalogBlocked && strings.TrimSpace(draft.ModelID) != "" {
		rows = append(rows, retained.Named[state.Model]("add-model/id", backendURLEditorRow(ctx, views.RowTargetAlias, selectors.EmptyOr(strings.TrimSpace(draft.TargetAlias), "not set"), strings.TrimSpace(draft.TargetAlias), "fast", func(value string) []update.Action {
			next := draft
			next.TargetAlias = strings.TrimSpace(strings.ToLower(value))
			panel.setDraft(next)
			return nil
		})))
	}
	if strings.EqualFold(strings.TrimSpace(draft.ProviderSpec), "custom") {
		rows = append(rows, retained.Named[state.Model]("add-model/backend-url", backendURLEditorRow(ctx, views.RowBackendURL, selectors.EmptyOr(strings.TrimSpace(draft.BaseURL), "missing"), strings.TrimSpace(draft.BaseURL), "https://host/v1", func(value string) []update.Action {
			next := draft
			next.BaseURL = strings.TrimSpace(value)
			panel.setDraft(next)
			return nil
		})))
	}
	if createRow := buildAddModelCreateRow(model, snapshot, draft, panel); createRow != nil {
		rows = append(rows, createRow)
	}
	return rows
}

func appendWorkspaceAddModelCredentialRows(
	ctx *retained.Context[state.Model],
	model state.Model,
	endpointName string,
	modelCatalogBlocked bool,
	rows []retained.ViewSpec[state.Model],
	draft state.ProviderConfigSnapshot,
	panel addModelPanelState,
) []retained.ViewSpec[state.Model] {
	providerSpec := strings.TrimSpace(draft.ProviderSpec)
	source := credentialSource(strings.TrimSpace(draft.CredentialRef))
	authViewState := addModelChatGPTAuthViewNone
	if strings.EqualFold(providerSpec, "chatgpt") {
		variant := providercatalog.AuthVariant(strings.ToLower(strings.TrimSpace(source)))
		if providercatalog.IsInteractiveAuthVariant(variant) {
			authViewState = classifyAddModelChatGPTAuthViewState(model, strings.TrimSpace(endpointName), draft, variant)
		}
	}
	rows = append(rows, interactiveAddModelCredentialRows(model, providerSpec, endpointName, draft, source)...)
	if strings.EqualFold(source, "env") {
		rows = append(rows, retained.Named[state.Model]("add-model/env-key", buildAddModelEnvKeyRow(ctx, model, draft, panel)))
		key := strings.TrimSpace(envCredentialKey(draft.CredentialRef))
		if key == "" {
			key = strings.TrimSpace(providercatalog.DefaultEnvKeyForSpec(providerSpec))
		}
		if key != "" {
			rows = append(rows, retained.Named[state.Model]("add-model/env-expected", views.RowStatic("expected", key)))
		}
	}
	if strings.EqualFold(source, "keychain") {
		rows = append(rows, retained.Named[state.Model]("add-model/keychain-key-name", buildAddModelKeychainKeyNameRow(ctx, model, draft, panel)))
		effective := keychainEffectiveName(providerSpec, keychainCredentialName(draft.CredentialRef))
		if strings.TrimSpace(keychainValueSummary(model, providerSpec, effective)) == "missing" {
			rows = append(rows, retained.Named[state.Model]("add-model/keychain-missing", views.RowStatic("keychain", "no key found")))
		}
	}
	if strings.EqualFold(source, "file") {
		rows = append(rows, retained.Named[state.Model]("add-model/credential-file", buildAddModelCredentialFileRow(ctx, draft, panel)))
		path := strings.TrimSpace(credentialFilePath(draft.CredentialRef))
		if path == "" {
			rows = append(rows, retained.Named[state.Model]("add-model/file-missing", views.RowStatic("key file", "not found")))
		} else if _, err := os.Stat(path); err != nil {
			rows = append(rows, retained.Named[state.Model]("add-model/file-missing", views.RowStatic("key file", "not found")))
		}
	}
	if modelCatalogBlocked && strings.TrimSpace(source) != "" &&
		!(strings.EqualFold(providerSpec, "chatgpt") && providercatalog.IsInteractiveAuthVariant(providercatalog.AuthVariant(strings.ToLower(strings.TrimSpace(source)))) && authViewState != addModelChatGPTAuthViewSignedIn) {
		authFailed := strings.TrimSpace(model.AuthLoginEndpointName) == strings.TrimSpace(endpointName) &&
			strings.TrimSpace(model.AuthLoginProviderRef) == strings.TrimSpace(draft.Ref) &&
			strings.EqualFold(strings.TrimSpace(model.AuthLoginSessionState), "failed")
		if !authFailed {
			if message := strings.TrimSpace(providerModelCatalogBlockedMessage(providerSpec, strings.TrimSpace(draft.BaseURL), strings.TrimSpace(draft.CredentialRef))); message != "" {
				rows = append(rows, views.DisclosureNoteRows(message)...)
			}
		}
	}
	return rows
}

func buildAddModelProviderRow(
	ctx *retained.Context[state.Model],
	model state.Model,
	endpointName string,
	draft state.ProviderConfigSnapshot,
	panel addModelPanelState,
) retained.ViewSpec[state.Model] {
	providerRow := views.RowActionWithCancel("provider", selectors.EmptyOr(providercatalog.DisplayName(strings.TrimSpace(draft.ProviderSpec)), "choose a provider"), "change", func() []update.Action {
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
	providerRowNamed := retained.Named[state.Model]("add-model/provider-row", providerRow)
	if !panel.providerPickerOpen {
		return providerRowNamed
	}
	items := buildAddModelProviderItems(model, endpointName, draft, panel)
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

func buildAddModelProviderItems(model state.Model, endpointName string, draft state.ProviderConfigSnapshot, panel addModelPanelState) []views.FilterablePickerItem {
	items := createProviderSpecItems(model, nil)
	for i := range items {
		item := items[i]
		spec := providerSpecFromSearch(item.Search)
		items[i].OnChoose = func() []update.Action {
			next := state.ProviderConfigForSpec(spec, draft)
			next.Ref = draft.Ref
			next.ModelID = ""
			next.TargetAlias = ""
			if strings.EqualFold(spec, "chatgpt") {
				next.CredentialRef = string(providercatalog.AuthVariantChatGPTLogin)
			}
			panel.setDraft(next)
			panel.setProviderPickerOpen(false)
			panel.setModelPickerOpen(false)
			panel.setCredentialUI(closeAddModelCredentialUIState(panel.credentialUI))
			focusKey := "add-model/model"
			if providerCredentialSelectionRequired(strings.TrimSpace(next.ProviderSpec), strings.TrimSpace(next.BaseURL), strings.TrimSpace(next.CredentialRef)) {
				focusKey = "add-model/credentials"
			}
			actions := []update.Action{
				state.SetInteractionMode{Mode: state.InteractionModeManageList},
				interaction.FocusKeyAction{Key: focusKey},
			}
			if strings.EqualFold(spec, "chatgpt") && strings.TrimSpace(endpointName) != "" {
				actions = append(startAuthActionsForAddModel(endpointName, next), actions...)
			}
			return actions
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

func buildAddModelCredentialRow(model state.Model, endpointName string, draft state.ProviderConfigSnapshot, panel addModelPanelState) retained.ViewSpec[state.Model] {
	summary := addModelCredentialSummary(model, draft)
	row := views.RowActionWithCancel("auth", selectors.EmptyOr(summary, "not set"), "change", func() []update.Action {
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
		variant := providercatalog.AuthVariant(strings.ToLower(strings.TrimSpace(choice)))
		if providercatalog.IsInteractiveAuthVariant(variant) {
			next.CredentialRef = string(variant)
			// Browser flow starts only when operator activates "sign in".
			if strings.EqualFold(string(variant), "chatgpt_login") {
				return []update.Action{state.ResetAuthLoginUIRequested{}}
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
	}, strings.TrimSpace(draft.ProviderSpec), false)
	return toolkitviews.NewAnchoredDisclosure(row, options...)
}

func addModelCredentialSummary(model state.Model, draft state.ProviderConfigSnapshot) string {
	resolvedRef := strings.TrimSpace(effectiveAddModelCredentialRef(model, draft))
	draftRef := strings.TrimSpace(draft.CredentialRef)
	providerSpec := strings.TrimSpace(draft.ProviderSpec)
	draftVariant := providercatalog.AuthVariant(strings.ToLower(strings.TrimSpace(credentialSource(draftRef))))
	if providercatalog.SupportsAuthVariant(providerSpec, draftVariant) &&
		providercatalog.IsInteractiveAuthVariant(draftVariant) &&
		resolvedRef != "" &&
		!strings.EqualFold(resolvedRef, draftRef) {
		return "signed in"
	}
	source := strings.TrimSpace(credentialSource(resolvedRef))
	if source == "" {
		return "missing"
	}
	variant := providercatalog.AuthVariant(strings.ToLower(source))
	if providercatalog.SupportsAuthVariant(providerSpec, variant) && providercatalog.IsInteractiveAuthVariant(variant) {
		if resolvedRef != "" && !strings.EqualFold(resolvedRef, string(variant)) {
			return "signed in"
		}
		return providercatalog.AuthVariantDisplayLabel(providerSpec, variant)
	}
	if strings.EqualFold(source, "env") {
		key := strings.TrimSpace(envCredentialKey(resolvedRef))
		if key == "" {
			key = strings.TrimSpace(providercatalog.DefaultEnvKeyForSpec(providerSpec))
		}
		if key != "" {
			if _, ok := os.LookupEnv(key); !ok {
				return "env var missing"
			}
		}
		return "env var"
	}
	if strings.EqualFold(source, "file") {
		path := strings.TrimSpace(credentialFilePath(resolvedRef))
		if path == "" {
			return "file missing"
		}
		if _, err := os.Stat(path); err != nil {
			return "file missing"
		}
		return "file"
	}
	if strings.EqualFold(source, "keychain") {
		effective := keychainEffectiveName(providerSpec, keychainCredentialName(resolvedRef))
		if strings.TrimSpace(keychainValueSummary(model, providerSpec, effective)) == "missing" {
			return "keychain missing"
		}
		return "keychain"
	}
	if providercatalog.SupportsAuthVariant(providerSpec, variant) {
		return providercatalog.AuthVariantDisplayLabel(providerSpec, variant)
	}
	return selectors.CredentialSummaryFromProviderConfig(&draft)
}

func interactiveAddModelCredentialRows(
	model state.Model,
	providerSpec string,
	endpointName string,
	draft state.ProviderConfigSnapshot,
	source string,
) []retained.ViewSpec[state.Model] {
	variant := providercatalog.AuthVariant(strings.ToLower(strings.TrimSpace(source)))
	if !providercatalog.SupportsAuthVariant(strings.TrimSpace(providerSpec), variant) || !providercatalog.IsInteractiveAuthVariant(variant) {
		return nil
	}
	return interactiveAuthStatusRows(model, interactiveAuthRenderConfig{
		EndpointName: strings.TrimSpace(endpointName),
		Draft:        draft,
		Variant:      variant,
		StartAuth: func(next state.ProviderConfigSnapshot) []update.Action {
			return startAuthActionsForAddModel(endpointName, next)
		},
		SwitchToDeviceAuth: func(next state.ProviderConfigSnapshot) []update.Action {
			return append([]update.Action{state.ResetAuthLoginUIRequested{}}, startAuthActionsForAddModel(endpointName, next)...)
		},
	})
}

type addModelChatGPTAuthViewState string

const (
	addModelChatGPTAuthViewNone               addModelChatGPTAuthViewState = "none"
	addModelChatGPTAuthViewBrowserNotStarted  addModelChatGPTAuthViewState = "browser_not_started"
	addModelChatGPTAuthViewInProgress         addModelChatGPTAuthViewState = "in_progress"
	addModelChatGPTAuthViewBrowserUnavailable addModelChatGPTAuthViewState = "browser_unavailable"
	addModelChatGPTAuthViewExpired            addModelChatGPTAuthViewState = "expired"
	addModelChatGPTAuthViewSignedIn           addModelChatGPTAuthViewState = "signed_in"
)

func classifyAddModelChatGPTAuthViewState(model state.Model, endpointName string, draft state.ProviderConfigSnapshot, variant providercatalog.AuthVariant) addModelChatGPTAuthViewState {
	if strings.EqualFold(addModelCredentialSummary(model, draft), "signed in") {
		return addModelChatGPTAuthViewSignedIn
	}
	if strings.EqualFold(strings.TrimSpace(model.AuthLoginSessionState), "expired") {
		return addModelChatGPTAuthViewExpired
	}
	if variant == providercatalog.AuthVariantChatGPTLogin &&
		strings.EqualFold(strings.TrimSpace(model.AuthLoginSessionState), "failed") &&
		strings.TrimSpace(model.AuthLoginSessionID) != "" {
		return addModelChatGPTAuthViewBrowserUnavailable
	}
	sessionActive := strings.TrimSpace(model.AuthLoginEndpointName) == strings.TrimSpace(endpointName) &&
		strings.TrimSpace(model.AuthLoginProviderRef) == strings.TrimSpace(draft.Ref) &&
		strings.TrimSpace(model.AuthLoginSessionID) != ""
	if sessionActive {
		return addModelChatGPTAuthViewInProgress
	}
	if variant == providercatalog.AuthVariantChatGPTLogin {
		return addModelChatGPTAuthViewBrowserNotStarted
	}
	return addModelChatGPTAuthViewNone
}

type interactiveAuthRenderConfig struct {
	EndpointName       string
	Draft              state.ProviderConfigSnapshot
	Variant            providercatalog.AuthVariant
	StartAuth          func(next state.ProviderConfigSnapshot) []update.Action
	SwitchToDeviceAuth func(next state.ProviderConfigSnapshot) []update.Action
}

func interactiveAuthStatusRows(model state.Model, cfg interactiveAuthRenderConfig) []retained.ViewSpec[state.Model] {
	rows := make([]retained.ViewSpec[state.Model], 0, 6)
	endpointName := strings.TrimSpace(cfg.EndpointName)
	draft := cfg.Draft
	variant := cfg.Variant
	viewState := classifyAddModelChatGPTAuthViewState(model, endpointName, draft, variant)
	if viewState == addModelChatGPTAuthViewInProgress || viewState == addModelChatGPTAuthViewBrowserUnavailable {
		stateValue := strings.TrimSpace(model.AuthLoginSessionState)
		loginURL := strings.TrimSpace(model.AuthLoginURL)
		userCode := strings.TrimSpace(model.AuthLoginUserCode)
		if variant == providercatalog.AuthVariantChatGPTLogin {
			if viewState == addModelChatGPTAuthViewBrowserUnavailable {
				rows = append(rows, views.RowStatic("", "could not open default browser"))
			}
		}
		if loginURL != "" {
			rows = append(rows, interactiveAuthLinkRows(loginURL)...)
		}
		if shouldRenderInteractiveAuthCode(variant, userCode) {
			rows = append(rows, views.RowAction("code", userCode, "copy", func() []update.Action {
				return []update.Action{
					state.AuthLoginURLCopyRequested{Value: userCode},
				}
			}))
		}
		if strings.EqualFold(stateValue, "pending") {
			rows = append(rows, views.RowStatic("", "waiting for sign-in..."))
		}
	} else if viewState == addModelChatGPTAuthViewBrowserNotStarted {
		rows = append(rows, views.RowAction("sign in", "open default browser", "open", func() []update.Action {
			if cfg.StartAuth == nil {
				return nil
			}
			return cfg.StartAuth(draft)
		}))
		loginURL := strings.TrimSpace(model.AuthLoginURL)
		if loginURL != "" {
			rows = append(rows, interactiveAuthLinkRows(loginURL)...)
		}
	}
	if viewState == addModelChatGPTAuthViewExpired {
		rows = append(rows, views.RowAction("code expired", "", "refresh", func() []update.Action {
			if cfg.StartAuth == nil {
				return nil
			}
			return cfg.StartAuth(draft)
		}))
	}
	if variant == providercatalog.AuthVariantChatGPTLogin &&
		(viewState == addModelChatGPTAuthViewBrowserNotStarted || viewState == addModelChatGPTAuthViewInProgress || viewState == addModelChatGPTAuthViewBrowserUnavailable) {
		rows = append(rows, views.RowAction("fallback", "use device code", "switch", func() []update.Action {
			next := draft
			next.CredentialRef = string(providercatalog.AuthVariantChatGPTDeviceAuth)
			if cfg.SwitchToDeviceAuth != nil {
				return cfg.SwitchToDeviceAuth(next)
			}
			if cfg.StartAuth != nil {
				return cfg.StartAuth(next)
			}
			return nil
		}))
	}
	if strings.TrimSpace(model.AuthLoginSessionError) != "" {
		rows = append(rows, views.DisclosureNoteRows(model.AuthLoginSessionError)...)
	}
	if strings.TrimSpace(model.AuthLoginSessionError) != "" && strings.TrimSpace(model.AuthLoginSessionID) == "" {
		rows = append(rows, views.DisclosureNoteRows("auth start failed; retry or switch auth method")...)
	}
	return rows
}

func shouldRenderInteractiveAuthCode(variant providercatalog.AuthVariant, userCode string) bool {
	return variant == providercatalog.AuthVariantChatGPTDeviceAuth && strings.TrimSpace(userCode) != ""
}

func interactiveAuthLinkRows(loginURL string) []retained.ViewSpec[state.Model] {
	url := strings.TrimSpace(loginURL)
	if url == "" {
		return nil
	}
	rows := []retained.ViewSpec[state.Model]{
		views.RowActionWideValue("link", "", "copy", func() []update.Action {
			return []update.Action{
				state.AuthLoginURLCopyRequested{Value: url},
			}
		}),
	}
	rows = append(rows, views.WrappedDetailRows(url)...)
	return rows
}

func startAuthActionsForAddModel(endpointName string, providerConfig state.ProviderConfigSnapshot) []update.Action {
	return startAuthActionsForFlow(endpointName, providerConfig, stateModel.AuthScopeEndpointProvider)
}

func startAuthActionsForCreateDraft(providerConfig state.ProviderConfigSnapshot) []update.Action {
	return startAuthActionsForFlow("", providerConfig, stateModel.AuthScopeCreateDraft)
}

func startAuthActionsForFlow(endpointName string, providerConfig state.ProviderConfigSnapshot, authScope string) []update.Action {
	return []update.Action{
		state.StartProviderAuthSessionRequested{
			EndpointName:   strings.TrimSpace(endpointName),
			ProviderConfig: providerConfig,
			AuthSubject:    stateModel.EncodeAuthTransientSubjectLocator(strings.TrimSpace(endpointName), strings.TrimSpace(providerConfig.Ref)),
			AuthScope:      strings.TrimSpace(authScope),
		},
	}
}

func buildAddModelEnvKeyRow(_ *retained.Context[state.Model], model state.Model, draft state.ProviderConfigSnapshot, panel addModelPanelState) retained.ViewSpec[state.Model] {
	return retained.Build(func(childCtx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
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
					state.LoadRoutingModelCatalogRequested{
						Scope:         state.RoutingModelCatalogScopeAddModelDraft,
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
				return []update.Action{state.StoreKeychainCredentialRequested{
					ProviderSpec: strings.TrimSpace(draft.ProviderSpec),
					KeyName:      effectiveName,
					Secret:       strings.TrimSpace(value),
				}}
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

func addModelCreateReady(draft state.ProviderConfigSnapshot) bool {
	requiresInteractiveAuth := false
	for _, variant := range providercatalog.SupportedAuthVariantsForSpec(strings.TrimSpace(draft.ProviderSpec)) {
		if providercatalog.IsInteractiveAuthVariant(variant) {
			requiresInteractiveAuth = true
			break
		}
	}
	return strings.TrimSpace(draft.ProviderSpec) != "" &&
		strings.TrimSpace(draft.ModelID) != "" &&
		(requiresInteractiveAuth || !providerCredentialSelectionRequired(draft.ProviderSpec, draft.BaseURL, draft.CredentialRef) || strings.TrimSpace(draft.CredentialRef) != "") &&
		!isEmptyFileCredentialRef(draft.CredentialRef) &&
		(!strings.EqualFold(strings.TrimSpace(draft.ProviderSpec), "custom") || strings.TrimSpace(draft.BaseURL) != "")
}

func effectiveAddModelCredentialRef(model state.Model, draft state.ProviderConfigSnapshot) string {
	ref := strings.TrimSpace(draft.CredentialRef)
	providerSpec := strings.TrimSpace(draft.ProviderSpec)
	interactiveVariants := make(map[string]struct{}, 2)
	for _, variant := range providercatalog.SupportedAuthVariantsForSpec(providerSpec) {
		if providercatalog.IsInteractiveAuthVariant(variant) {
			interactiveVariants[strings.ToLower(strings.TrimSpace(string(variant)))] = struct{}{}
		}
	}
	if len(interactiveVariants) == 0 {
		return ref
	}
	if ref != "" {
		if _, ok := interactiveVariants[strings.ToLower(ref)]; !ok {
			return ref
		}
	}
	if strings.TrimSpace(model.AddModelDraftProviderSpec) != providerSpec {
		return ref
	}
	if strings.TrimSpace(model.AddModelDraftBaseURL) != strings.TrimSpace(draft.BaseURL) {
		return ref
	}
	resolved := strings.TrimSpace(model.AddModelDraftCredentialRef)
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
