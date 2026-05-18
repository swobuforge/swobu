package routing

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

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
	if strings.TrimSpace(draft.Ref) == "" { // swobu:io-string source=boundary
		draft.Ref = nextProviderDraftKey(snapshot)
	}
	providerSpec := strings.TrimSpace(draft.ProviderSpec) // swobu:io-string source=boundary
	rows := make([]retained.ViewSpec[state.Model], 0, 12)
	rows = appendCanonicalProviderConfigLayout(rows, "add-model", canonicalProviderConfigLayout{
		Provider:   buildAddModelProviderRow(ctx, model, strings.TrimSpace(snapshot.Name), draft, panel), // swobu:io-string source=boundary
		Credential: buildAddModelCredentialRow(model, strings.TrimSpace(snapshot.Name), draft, panel),    // swobu:io-string source=boundary
		Scope:      buildAddModelScopeRow(ctx, draft, panel),
	})
	effectiveCredentialRef := effectiveAddModelCredentialRef(model, draft)
	authVariant := providercatalog.AuthVariant(strings.ToLower(strings.TrimSpace(credentialSource(strings.TrimSpace(draft.CredentialRef))))) // swobu:io-string source=boundary
	authViewState := interactiveAuthPhaseNone
	if strings.EqualFold(providerSpec, "chatgpt") && providercatalog.IsInteractiveAuthVariant(authVariant) {
		authViewState = classifyInteractiveAuthPhase(model, strings.TrimSpace(snapshot.Name), draft, authVariant) // swobu:io-string source=boundary
	}
	modelCatalogBlocked := state.ProviderModelCatalogLoadBlocked(
		providerSpec,
		strings.TrimSpace(draft.BaseURL), // swobu:io-string source=boundary
		effectiveCredentialRef,
	)
	rows = appendWorkspaceAddModelCredentialRows(ctx, model, strings.TrimSpace(snapshot.Name), modelCatalogBlocked, rows, draft, panel) // swobu:io-string source=boundary
	if providerSpec != "" && !modelCatalogBlocked {
		rows = append(rows, retained.Named[state.Model]("add-model/model", buildAddModelModelChoiceRow(ctx, model, panel)))
	} else if providerSpec != "" && modelCatalogBlocked &&
		!(strings.EqualFold(providerSpec, "chatgpt") && providercatalog.IsInteractiveAuthVariant(authVariant) && authViewState != interactiveAuthPhaseResolved) {
		rows = append(rows, retained.Named[state.Model]("add-model/model-blocked", views.RowStatic("model", "choose after auth")))
	}
	if providerSpec != "" && !modelCatalogBlocked && strings.TrimSpace(draft.ModelID) != "" { // swobu:io-string source=boundary
		rows = append(rows, retained.Named[state.Model]("add-model/id", aliasInlineEditorRow(ctx, selectors.EmptyOr(strings.TrimSpace(draft.TargetAlias), "not set"), strings.TrimSpace(draft.TargetAlias), "fast", func(value string) []update.Action { // swobu:io-string source=boundary
			next := draft
			next.TargetAlias = strings.TrimSpace(strings.ToLower(value)) // swobu:io-string source=boundary
			panel.setDraft(next)
			return nil
		})))
	}
	rows = append(rows, retained.Named[state.Model]("add-model/delivery", buildAddModelFrameRow(draft, panel)))
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
		selected := strings.TrimSpace(draft.SelectedFrame) // swobu:io-string source=boundary
		if selected == "" && len(frames) > 0 {
			selected = frames[0]
		}
		if selected == "" {
			selected = "not set"
		}
		summary := "auto"
		if strings.TrimSpace(draft.ModelID) != "" { // swobu:io-string source=boundary
			summary = presentDeliveryFrameForProvider(
				draft.ProviderSpec,
				protocolkind.ProtocolKind(defaultProtocolKindForProvider(draft.ProviderSpec)),
				selected,
			)
		}
		return views.RowActionWithCancel(
			providerDeliveryRowLabel,
			summary,
			"next",
			func() []update.Action {
				next := nextFrameSelection(frames, strings.TrimSpace(draft.SelectedFrame)) // swobu:io-string source=boundary
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

func buildAddModelScopeRow(ctx *retained.Context[state.Model], draft state.ProviderConfigSnapshot, panel addModelPanelState) retained.ViewSpec[state.Model] {
	if strings.EqualFold(strings.TrimSpace(draft.ProviderSpec), "bedrock") { // swobu:io-string source=boundary
		return addModelBedrockAuthRegionEditor(ctx, draft, panel)
	}
	if strings.EqualFold(strings.TrimSpace(draft.ProviderSpec), "openai_compatible") { // swobu:io-string source=boundary
		return backendURLEditorRow(ctx, "scope", selectors.EmptyOr(strings.TrimSpace(draft.BaseURL), "base url missing"), strings.TrimSpace(draft.BaseURL), "https://host/v1", func(value string) []update.Action { // swobu:io-string source=boundary
			next := draft
			next.BaseURL = strings.TrimSpace(value) // swobu:io-string source=boundary
			panel.setDraft(next)
			return nil
		})
	}
	return nil
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
	providerSpec := strings.TrimSpace(draft.ProviderSpec)              // swobu:io-string source=boundary
	source := credentialSource(strings.TrimSpace(draft.CredentialRef)) // swobu:io-string source=boundary
	authViewState := interactiveAuthPhaseNone
	if strings.EqualFold(providerSpec, "chatgpt") {
		variant := providercatalog.AuthVariant(strings.ToLower(strings.TrimSpace(source))) // swobu:io-string source=boundary
		if providercatalog.IsInteractiveAuthVariant(variant) {
			authViewState = classifyInteractiveAuthPhase(model, strings.TrimSpace(endpointName), draft, variant) // swobu:io-string source=boundary
		}
	}
	rows = append(rows, interactiveAddModelCredentialRows(model, providerSpec, endpointName, draft, source)...)
	if strings.EqualFold(source, "env") {
		if strings.EqualFold(providerSpec, "bedrock") {
			rows = append(rows, retained.Named[state.Model]("add-model/env-key", addModelBedrockAuthEnvEditor(ctx, draft, panel)))
		} else {
			rows = append(rows, retained.Named[state.Model]("add-model/env-key", buildAddModelEnvKeyRow(ctx, model, draft, panel)))
		}
		rows = append(rows, authModeRendererForCredentialRef(draft.CredentialRef).RenderAddModelExtras(providerSpec, draft.CredentialRef)...)
	}
	if strings.EqualFold(providerSpec, "bedrock") {
		rows = append(rows, retained.Named[state.Model]("add-model/profile", addModelBedrockAuthProfileEditor(ctx, draft, panel)))
		rows = append(rows, retained.Named[state.Model]("add-model/region", addModelBedrockAuthRegionEditor(ctx, draft, panel)))
	}
	if strings.EqualFold(source, "file") {
		rows = append(rows, retained.Named[state.Model]("add-model/credential-file", buildAddModelCredentialFileRow(ctx, draft, panel)))
		rows = append(rows, authModeRendererForCredentialRef(draft.CredentialRef).RenderAddModelExtras(providerSpec, draft.CredentialRef)...)
	}
	if modelCatalogBlocked && strings.TrimSpace(source) != "" && // swobu:io-string source=boundary
		!(strings.EqualFold(providerSpec, "chatgpt") && providercatalog.IsInteractiveAuthVariant(providercatalog.AuthVariant(strings.ToLower(strings.TrimSpace(source)))) && authViewState != interactiveAuthPhaseResolved) { // swobu:io-string source=boundary
		authState := addModelAuthStateForDraft(model, endpointName, draft)
		authFailed := strings.EqualFold(strings.TrimSpace(authState.SessionState), "failed") // swobu:io-string source=boundary
		if !authFailed {
			if message := strings.TrimSpace(state.ProviderModelCatalogBlockedMessage(providerSpec, strings.TrimSpace(draft.BaseURL), strings.TrimSpace(draft.CredentialRef))); message != "" { // swobu:io-string source=boundary
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
	providerRow := views.RowActionWithCancel("provider", selectors.EmptyOr(providerDisplayName(strings.TrimSpace(draft.ProviderSpec)), "choose a provider"), "change", func() []update.Action { // swobu:io-string source=boundary
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
			if strings.EqualFold(spec, "bedrock") && strings.TrimSpace(next.BaseURL) == "" { // swobu:io-string source=boundary
				if region := strings.TrimSpace(bedrockRegionFromEnv()); region != "" { // swobu:io-string source=boundary
					next.Region = region
					next.BaseURL = bedrockBaseURLForRegion(region)
				}
			}
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
			if state.ProviderCredentialSelectionRequired(strings.TrimSpace(next.ProviderSpec), strings.TrimSpace(next.BaseURL), strings.TrimSpace(next.CredentialRef)) { // swobu:io-string source=boundary
				focusKey = "add-model/credentials"
			}
			actions := []update.Action{
				state.SetInteractionMode{Mode: state.InteractionModeManageList},
				interaction.FocusKeyAction{Key: focusKey},
			}
			if strings.EqualFold(spec, "chatgpt") && strings.TrimSpace(endpointName) != "" { // swobu:io-string source=boundary
				actions = append(startAuthActionsForAddModel(endpointName, next), actions...)
			}
			return actions
		}
	}
	return items
}

func providerSpecFromSearch(search string) string {
	spec := strings.TrimSpace(strings.ToLower(search)) // swobu:io-string source=boundary
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
