package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
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
			setExpandedRef(strings.TrimSpace(snapshot.SelectedProviderConfigRef)) // swobu:io-string source=boundary
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
	endpointName := strings.TrimSpace(snapshot.Name)                     // swobu:io-string source=boundary
	selectedRef := strings.TrimSpace(snapshot.SelectedProviderConfigRef) // swobu:io-string source=boundary
	for _, pc := range snapshot.ProviderConfigs {
		ref := strings.TrimSpace(pc.Ref)                    // swobu:io-string source=boundary
		isExpanded := strings.TrimSpace(expandedRef) == ref // swobu:io-string source=boundary
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
	ref = strings.TrimSpace(ref) // swobu:io-string source=boundary
	if ref == "" {
		return "default"
	}
	return ref
}
