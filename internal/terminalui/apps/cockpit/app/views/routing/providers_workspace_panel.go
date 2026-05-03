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
	open                    bool
	setOpen                 func(bool)
	draft                   state.ProviderConfigSnapshot
	setDraft                func(state.ProviderConfigSnapshot)
	providerPicker          views.FilterablePickerState
	setProviderPicker       func(views.FilterablePickerState)
	providerPickerOpen      bool
	setProviderPickerOpen   func(bool)
	credentialPickerOpen    bool
	setCredentialPickerOpen func(bool)
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
	addCredentialPickerOpen, setAddCredentialPickerOpen := view.UseState(ctx, func() bool { return false })

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
		open:                    addOpen,
		setOpen:                 setAddOpen,
		draft:                   addDraft,
		setDraft:                setAddDraft,
		providerPicker:          addProviderPicker,
		setProviderPicker:       setAddProviderPicker,
		providerPickerOpen:      addProviderPickerOpen,
		setProviderPickerOpen:   setAddProviderPickerOpen,
		credentialPickerOpen:    addCredentialPickerOpen,
		setCredentialPickerOpen: setAddCredentialPickerOpen,
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
		panel.setCredentialPickerOpen(false)
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
	panel.setCredentialPickerOpen(false)
	views.ResetFilterablePickerState(panel.setProviderPicker)
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
		view.Named[state.Model]("add-model/model", backendURLEditorRow(ctx, views.RowModel, selectors.EmptyOr(strings.TrimSpace(draft.ModelID), "not set"), strings.TrimSpace(draft.ModelID), "gpt-5.3", func(value string) []update.Action {
			next := draft
			next.ModelID = strings.TrimSpace(value)
			panel.setDraft(next)
			return nil
		})),
		view.Named[state.Model]("add-model/id", backendURLEditorRow(ctx, views.RowTargetAlias, selectors.EmptyOr(strings.TrimSpace(draft.TargetAlias), "not set"), strings.TrimSpace(draft.TargetAlias), "fast", func(value string) []update.Action {
			next := draft
			next.TargetAlias = strings.TrimSpace(strings.ToLower(value))
			panel.setDraft(next)
			return nil
		})),
	}
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
			panel.setCredentialPickerOpen(false)
			return []update.Action{
				state.SetInteractionMode{Mode: state.InteractionModeManageList},
				interaction.FocusKeyAction{Key: "add-model/provider-row"},
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
		panel.setCredentialPickerOpen(!panel.credentialPickerOpen)
		return nil
	}, nil)
	if !panel.credentialPickerOpen {
		return row
	}
	options := credentialOptionRows(credentialSource(draft.CredentialRef), func(choice string) []update.Action {
		next := draft
		ref := strings.TrimSpace(choice)
		if strings.EqualFold(ref, "env") {
			ref = encodeCredentialEnvRef(providercatalog.DefaultEnvKeyForSpec(strings.TrimSpace(next.ProviderSpec)))
		}
		if strings.EqualFold(ref, "file") {
			ref = encodeCredentialFileRef("")
		}
		next.CredentialRef = ref
		panel.setDraft(next)
		panel.setCredentialPickerOpen(false)
		return nil
	}, func() []update.Action {
		panel.setCredentialPickerOpen(false)
		return nil
	})
	return toolkitviews.NewAnchoredDisclosure(row, options...)
}

func buildAddModelCreateRow(snapshot *state.EndpointSnapshot, draft state.ProviderConfigSnapshot, panel addModelPanelState) view.ViewSpec[state.Model] {
	ready := strings.TrimSpace(draft.ProviderSpec) != "" &&
		strings.TrimSpace(draft.ModelID) != "" &&
		(!providerCredentialSelectionRequired(draft.ProviderSpec, draft.BaseURL, draft.CredentialRef) || strings.TrimSpace(draft.CredentialRef) != "") &&
		(!strings.EqualFold(strings.TrimSpace(draft.ProviderSpec), "custom") || strings.TrimSpace(draft.BaseURL) != "")
	if !ready {
		return view.Named[state.Model]("add-model/create", views.RowStatic("create model", "not ready"))
	}
	createDraft := draft
	return view.Named[state.Model]("add-model/create", views.RowAction("create model", "", "create", func() []update.Action {
		panel.setOpen(false)
		panel.setCredentialPickerOpen(false)
		return []update.Action{
			state.RoutingSaveStartedAction{},
			state.AddProviderConfigRequested{
				EndpointName:   strings.TrimSpace(snapshot.Name),
				ProviderConfig: createDraft,
			},
		}
	}))
}
