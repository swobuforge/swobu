package routing

import (
	"fmt"
	"os"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
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
		return state.ProviderConfigSnapshot{
			Ref:           nextProviderDraftKey(snapshot),
			ProtocolKind:  defaultProtocolKindForProvider(""),
			SelectedFrame: defaultSelectedFrameForProvider(""),
		}
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
				interaction.FocusKeyAction{Key: "provider-summary/" + stableProviderRowKey(snapshot.SelectedProviderConfigRef)},
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
	endpointName := strings.TrimSpace(snapshot.Name)                     // trimlowerlint:allow boundary canonicalization
	selectedRef := strings.TrimSpace(snapshot.SelectedProviderConfigRef) // trimlowerlint:allow boundary canonicalization
	for _, pc := range snapshot.ProviderConfigs {
		ref := strings.TrimSpace(pc.Ref)                    // trimlowerlint:allow boundary canonicalization
		isExpanded := strings.TrimSpace(expandedRef) == ref // trimlowerlint:allow boundary canonicalization
		choice := ref
		closeExpanded := func() []update.Action {
			if !isExpanded {
				return nil
			}
			setExpandedRef("")
			return []update.Action{
				state.SetInteractionMode{Mode: state.InteractionModeManageList},
				interaction.FocusKeyAction{Key: "provider-summary/" + stableProviderRowKey(ref)},
			}
		}
		parent := retained.Named[state.Model]("provider-summary/"+stableProviderRowKey(ref), newProviderSummaryRow(
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

func stableProviderRowKey(ref string) string {
	ref = strings.TrimSpace(ref) // trimlowerlint:allow boundary canonicalization
	if ref == "" {
		return "default"
	}
	return ref
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
	draft := state.ProviderConfigSnapshot{
		Ref:           nextProviderDraftKey(snapshot),
		ProtocolKind:  defaultProtocolKindForProvider(""),
		SelectedFrame: defaultSelectedFrameForProvider(""),
	}
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
	if strings.TrimSpace(draft.Ref) == "" { // trimlowerlint:allow boundary canonicalization
		draft.Ref = nextProviderDraftKey(snapshot)
	}
	providerSpec := strings.TrimSpace(draft.ProviderSpec) // trimlowerlint:allow boundary canonicalization
	rows := make([]retained.ViewSpec[state.Model], 0, 12)
	rows = appendCanonicalProviderConfigRows(rows, "add-model", canonicalProviderConfigRows{
		Provider: buildAddModelProviderRow(ctx, model, strings.TrimSpace(snapshot.Name), draft, panel),         // trimlowerlint:allow boundary canonicalization
		Auth:     buildAddModelCredentialRow(model, strings.TrimSpace(snapshot.Name), draft, panel),            // trimlowerlint:allow boundary canonicalization
		Frame:    buildAddModelFrameRow(draft, panel),
	})
	effectiveCredentialRef := effectiveAddModelCredentialRef(model, draft)
	authVariant := providercatalog.AuthVariant(strings.ToLower(strings.TrimSpace(credentialSource(strings.TrimSpace(draft.CredentialRef))))) // trimlowerlint:allow boundary canonicalization
	authViewState := interactiveAuthPhaseNone
	if strings.EqualFold(providerSpec, "chatgpt") && providercatalog.IsInteractiveAuthVariant(authVariant) {
		authViewState = classifyInteractiveAuthPhase(model, strings.TrimSpace(snapshot.Name), draft, authVariant) // trimlowerlint:allow boundary canonicalization
	}
	modelCatalogBlocked := providerModelCatalogLoadBlocked(
		providerSpec,
		strings.TrimSpace(draft.BaseURL), // trimlowerlint:allow boundary canonicalization
		effectiveCredentialRef,
	)
	rows = appendWorkspaceAddModelCredentialRows(ctx, model, strings.TrimSpace(snapshot.Name), modelCatalogBlocked, rows, draft, panel) // trimlowerlint:allow boundary canonicalization
	if providerSpec != "" && !modelCatalogBlocked {
		rows = append(rows, retained.Named[state.Model]("add-model/model", buildAddModelModelChoiceRow(ctx, model, panel)))
	} else if providerSpec != "" && modelCatalogBlocked &&
		!(strings.EqualFold(providerSpec, "chatgpt") && providercatalog.IsInteractiveAuthVariant(authVariant) && authViewState != interactiveAuthPhaseResolved) {
		rows = append(rows, retained.Named[state.Model]("add-model/model-blocked", views.RowStatic("model", "choose after auth")))
	}
	if providerSpec != "" && !modelCatalogBlocked && strings.TrimSpace(draft.ModelID) != "" { // trimlowerlint:allow boundary canonicalization
		rows = append(rows, retained.Named[state.Model]("add-model/id", aliasInlineEditorRow(ctx, selectors.EmptyOr(strings.TrimSpace(draft.TargetAlias), "not set"), strings.TrimSpace(draft.TargetAlias), "fast", func(value string) []update.Action { // trimlowerlint:allow boundary canonicalization
			next := draft
			next.TargetAlias = strings.TrimSpace(strings.ToLower(value)) // trimlowerlint:allow boundary canonicalization
			panel.setDraft(next)
			return nil
		})))
	}
	if strings.EqualFold(strings.TrimSpace(draft.ProviderSpec), "openai_compatible") { // trimlowerlint:allow boundary canonicalization
		rows = append(rows, retained.Named[state.Model]("add-model/backend-url", backendURLEditorRow(ctx, views.RowBackendURL, selectors.EmptyOr(strings.TrimSpace(draft.BaseURL), "missing"), strings.TrimSpace(draft.BaseURL), "https://host/v1", func(value string) []update.Action { // trimlowerlint:allow boundary canonicalization
			next := draft
			next.BaseURL = strings.TrimSpace(value) // trimlowerlint:allow boundary canonicalization
			panel.setDraft(next)
			return nil
		})))
	}
	if createRow := buildAddModelCreateRow(model, snapshot, draft, panel); createRow != nil {
		rows = append(rows, createRow)
	}
	return rows
}

func buildAddModelFrameRow(draft state.ProviderConfigSnapshot, panel addModelPanelState) retained.ViewSpec[state.Model] {
	return retained.Build[state.Model](func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		_ = ctx
		frames := providercatalog.SupportedFramesForSpecProtocol(
			draft.ProviderSpec,
			protocolkind.ProtocolKind(defaultProtocolKindForProvider(draft.ProviderSpec)),
		)
		selected := strings.TrimSpace(draft.SelectedFrame) // trimlowerlint:allow boundary canonicalization
		if selected == "" && len(frames) > 0 {
			selected = frames[0]
		}
		if selected == "" {
			selected = "not set"
		}
		return views.RowActionWithCancel(
			providerDeliveryRowLabel,
			presentDeliveryFrameForProvider(
				draft.ProviderSpec,
				protocolkind.ProtocolKind(defaultProtocolKindForProvider(draft.ProviderSpec)),
				selected,
			),
			"next",
			func() []update.Action {
			next := nextFrameSelection(frames, strings.TrimSpace(draft.SelectedFrame)) // trimlowerlint:allow boundary canonicalization
			if next == "" {
				return nil
			}
			updated := draft
			updated.SelectedFrame = next
			panel.setDraft(updated)
			return nil
			},
			nil,
		)
	})
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
	providerSpec := strings.TrimSpace(draft.ProviderSpec)              // trimlowerlint:allow boundary canonicalization
	source := credentialSource(strings.TrimSpace(draft.CredentialRef)) // trimlowerlint:allow boundary canonicalization
	authViewState := interactiveAuthPhaseNone
	if strings.EqualFold(providerSpec, "chatgpt") {
		variant := providercatalog.AuthVariant(strings.ToLower(strings.TrimSpace(source))) // trimlowerlint:allow boundary canonicalization
		if providercatalog.IsInteractiveAuthVariant(variant) {
			authViewState = classifyInteractiveAuthPhase(model, strings.TrimSpace(endpointName), draft, variant) // trimlowerlint:allow boundary canonicalization
		}
	}
	rows = append(rows, interactiveAddModelCredentialRows(model, providerSpec, endpointName, draft, source)...)
	if strings.EqualFold(source, "env") {
		rows = append(rows, retained.Named[state.Model]("add-model/env-key", buildAddModelEnvKeyRow(ctx, model, draft, panel)))
		key := strings.TrimSpace(envCredentialKey(draft.CredentialRef)) // trimlowerlint:allow boundary canonicalization
		if key == "" {
			key = strings.TrimSpace(providercatalog.DefaultEnvKeyForSpec(providerSpec)) // trimlowerlint:allow boundary canonicalization
		}
		if key != "" {
			rows = append(rows, retained.Named[state.Model]("add-model/env-expected", views.RowStatic("expected", key)))
		}
	}
	if strings.EqualFold(source, "file") {
		rows = append(rows, retained.Named[state.Model]("add-model/credential-file", buildAddModelCredentialFileRow(ctx, draft, panel)))
		path := strings.TrimSpace(credentialFilePath(draft.CredentialRef)) // trimlowerlint:allow boundary canonicalization
		if path == "" {
			rows = append(rows, retained.Named[state.Model]("add-model/file-missing", views.RowStatic("key file", "not found")))
		} else if _, err := os.Stat(path); err != nil {
			rows = append(rows, retained.Named[state.Model]("add-model/file-missing", views.RowStatic("key file", "not found")))
		}
	}
	if modelCatalogBlocked && strings.TrimSpace(source) != "" && // trimlowerlint:allow boundary canonicalization
		!(strings.EqualFold(providerSpec, "chatgpt") && providercatalog.IsInteractiveAuthVariant(providercatalog.AuthVariant(strings.ToLower(strings.TrimSpace(source)))) && authViewState != interactiveAuthPhaseResolved) { // trimlowerlint:allow boundary canonicalization
		authState := addModelAuthStateForDraft(model, endpointName, draft)
		authFailed := strings.EqualFold(strings.TrimSpace(authState.SessionState), "failed") // trimlowerlint:allow boundary canonicalization
		if !authFailed {
			if message := strings.TrimSpace(providerModelCatalogBlockedMessage(providerSpec, strings.TrimSpace(draft.BaseURL), strings.TrimSpace(draft.CredentialRef))); message != "" { // trimlowerlint:allow boundary canonicalization
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
	providerRow := views.RowActionWithCancel("provider", selectors.EmptyOr(providerDisplayName(strings.TrimSpace(draft.ProviderSpec)), "choose a provider"), "change", func() []update.Action { // trimlowerlint:allow boundary canonicalization
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
			if providerCredentialSelectionRequired(strings.TrimSpace(next.ProviderSpec), strings.TrimSpace(next.BaseURL), strings.TrimSpace(next.CredentialRef)) { // trimlowerlint:allow boundary canonicalization
				focusKey = "add-model/credentials"
			}
			actions := []update.Action{
				state.SetInteractionMode{Mode: state.InteractionModeManageList},
				interaction.FocusKeyAction{Key: focusKey},
			}
			if strings.EqualFold(spec, "chatgpt") && strings.TrimSpace(endpointName) != "" { // trimlowerlint:allow boundary canonicalization
				actions = append(startAuthActionsForAddModel(endpointName, next), actions...)
			}
			return actions
		}
	}
	return items
}

func providerSpecFromSearch(search string) string {
	spec := strings.TrimSpace(strings.ToLower(search)) // trimlowerlint:allow boundary canonicalization
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
		if strings.EqualFold(strings.TrimSpace(choice), "file") { // trimlowerlint:allow boundary canonicalization
			currentPath := credentialFilePath(next.CredentialRef)
			nextUI.FileBrowse = initialCredentialFileBrowseState(currentPath)
			nextUI.FilePicker = views.DefaultFilterablePickerState()
		}
		panel.setCredentialUI(nextUI)
		variant := providercatalog.AuthVariant(strings.ToLower(strings.TrimSpace(choice))) // trimlowerlint:allow boundary canonicalization
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
	}, strings.TrimSpace(draft.ProviderSpec), false) // trimlowerlint:allow boundary canonicalization
	return toolkitviews.NewAnchoredDisclosure(row, options...)
}

func addModelCredentialSummary(model state.Model, draft state.ProviderConfigSnapshot) string {
	resolvedRef := strings.TrimSpace(effectiveAddModelCredentialRef(model, draft))                              // trimlowerlint:allow boundary canonicalization
	draftRef := strings.TrimSpace(draft.CredentialRef)                                                          // trimlowerlint:allow boundary canonicalization
	providerSpec := strings.TrimSpace(draft.ProviderSpec)                                                       // trimlowerlint:allow boundary canonicalization
	draftVariant := providercatalog.AuthVariant(strings.ToLower(strings.TrimSpace(credentialSource(draftRef)))) // trimlowerlint:allow boundary canonicalization
	if providercatalog.SupportsAuthVariant(providerSpec, draftVariant) &&
		providercatalog.IsInteractiveAuthVariant(draftVariant) &&
		resolvedRef != "" &&
		!strings.EqualFold(resolvedRef, draftRef) {
		return "signed in"
	}
	source := strings.TrimSpace(credentialSource(resolvedRef)) // trimlowerlint:allow boundary canonicalization
	if source == "" {
		return "missing"
	}
	variant := providercatalog.AuthVariant(strings.ToLower(source)) // trimlowerlint:allow boundary canonicalization
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
		key := strings.TrimSpace(envCredentialKey(resolvedRef)) // trimlowerlint:allow boundary canonicalization
		if key == "" {
			key = strings.TrimSpace(providercatalog.DefaultEnvKeyForSpec(providerSpec)) // trimlowerlint:allow boundary canonicalization
		}
		if key != "" {
			if _, ok := os.LookupEnv(key); !ok {
				return "env var missing"
			}
		}
		return "env var"
	}
	if strings.EqualFold(source, "file") {
		path := strings.TrimSpace(credentialFilePath(resolvedRef)) // trimlowerlint:allow boundary canonicalization
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
	ownerKey := stateModel.EndpointProviderAuthOwnerKey(strings.TrimSpace(endpointName), strings.TrimSpace(providerConfig.Ref)).String() // trimlowerlint:allow boundary canonicalization
	if strings.TrimSpace(authScope) == stateModel.AuthScopeCreateDraft {                                                                 // trimlowerlint:allow boundary canonicalization
		ownerKey = stateModel.CreateDraftAuthOwnerKey(strings.TrimSpace(providerConfig.Ref)).String() // trimlowerlint:allow boundary canonicalization
	} else {
		ownerKey = stateModel.AddModelDraftAuthOwnerKey(strings.TrimSpace(endpointName), strings.TrimSpace(providerConfig.Ref)).String() // trimlowerlint:allow boundary canonicalization
	}
	return []update.Action{
		state.StartProviderAuthSessionRequested{
			EndpointName:   strings.TrimSpace(endpointName), // trimlowerlint:allow boundary canonicalization
			ProviderConfig: providerConfig,
			OwnerKey:       ownerKey,
			AuthScope:      strings.TrimSpace(authScope), // trimlowerlint:allow boundary canonicalization
		},
	}
}

func buildAddModelEnvKeyRow(_ *retained.Context[state.Model], model state.Model, draft state.ProviderConfigSnapshot, panel addModelPanelState) retained.ViewSpec[state.Model] {
	return retained.Build(func(childCtx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		current := strings.TrimSpace(envCredentialKey(draft.CredentialRef))                   // trimlowerlint:allow boundary canonicalization
		summary, editorValue := envKeySummary(strings.TrimSpace(draft.ProviderSpec), current) // trimlowerlint:allow boundary canonicalization
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
						ProviderSpec:  strings.TrimSpace(next.ProviderSpec),  // trimlowerlint:allow boundary canonicalization
						BaseURL:       strings.TrimSpace(next.BaseURL),       // trimlowerlint:allow boundary canonicalization
						CredentialRef: strings.TrimSpace(next.CredentialRef), // trimlowerlint:allow boundary canonicalization // trimlowerlint:allow boundary canonicalization
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
				return routingStoreKeychainCredentialActions(strings.TrimSpace(draft.ProviderSpec), effectiveName, strings.TrimSpace(value), "add-model/keychain") // trimlowerlint:allow boundary canonicalization
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
	ref := strings.TrimSpace(choice) // trimlowerlint:allow boundary canonicalization
	if strings.EqualFold(ref, "env") {
		ref = encodeCredentialEnvRef(providercatalog.DefaultEnvKeyForSpec(strings.TrimSpace(next.ProviderSpec))) // trimlowerlint:allow boundary canonicalization
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
		return routingAddProviderConfigActions(strings.TrimSpace(snapshot.Name), createDraft, "add-model/create") // trimlowerlint:allow boundary canonicalization
	}))
}

func isEmptyFileCredentialRef(ref string) bool {
	trimmed := strings.TrimSpace(ref) // trimlowerlint:allow boundary canonicalization
	if strings.EqualFold(trimmed, "file") {
		return true
	}
	return strings.HasPrefix(strings.ToLower(trimmed), fileCredentialRefPrefix) && strings.TrimSpace(credentialFilePath(trimmed)) == "" // trimlowerlint:allow boundary canonicalization
}

func addModelCreateReady(draft state.ProviderConfigSnapshot) bool {
	requiresInteractiveAuth := false
	for _, variant := range providercatalog.SupportedAuthVariantsForSpec(strings.TrimSpace(draft.ProviderSpec)) { // trimlowerlint:allow boundary canonicalization
		if providercatalog.IsInteractiveAuthVariant(variant) {
			requiresInteractiveAuth = true
			break
		}
	}
	return strings.TrimSpace(draft.ProviderSpec) != "" && // trimlowerlint:allow boundary canonicalization
		strings.TrimSpace(draft.ModelID) != "" && // trimlowerlint:allow boundary canonicalization
		(requiresInteractiveAuth || !providerCredentialSelectionRequired(draft.ProviderSpec, draft.BaseURL, draft.CredentialRef) || strings.TrimSpace(draft.CredentialRef) != "") && // trimlowerlint:allow boundary canonicalization
		!isEmptyFileCredentialRef(draft.CredentialRef) &&
		(!strings.EqualFold(strings.TrimSpace(draft.ProviderSpec), "openai_compatible") || strings.TrimSpace(draft.BaseURL) != "") // trimlowerlint:allow boundary canonicalization
}

func effectiveAddModelCredentialRef(model state.Model, draft state.ProviderConfigSnapshot) string {
	ref := strings.TrimSpace(draft.CredentialRef)         // trimlowerlint:allow boundary canonicalization
	providerSpec := strings.TrimSpace(draft.ProviderSpec) // trimlowerlint:allow boundary canonicalization
	interactiveVariants := make(map[string]struct{}, 2)
	for _, variant := range providercatalog.SupportedAuthVariantsForSpec(providerSpec) {
		if providercatalog.IsInteractiveAuthVariant(variant) {
			interactiveVariants[strings.ToLower(strings.TrimSpace(string(variant)))] = struct{}{} // trimlowerlint:allow boundary canonicalization
		}
	}
	if len(interactiveVariants) == 0 {
		return ref
	}
	if ref != "" {
		if _, ok := interactiveVariants[strings.ToLower(ref)]; !ok { // trimlowerlint:allow boundary canonicalization
			return ref
		}
	}
	if strings.TrimSpace(model.AddModelDraftProviderSpec) != providerSpec { // trimlowerlint:allow boundary canonicalization
		return ref
	}
	if strings.TrimSpace(model.AddModelDraftBaseURL) != strings.TrimSpace(draft.BaseURL) { // trimlowerlint:allow boundary canonicalization
		return ref
	}
	resolved := strings.TrimSpace(model.AddModelDraftCredentialRef) // trimlowerlint:allow boundary canonicalization
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
