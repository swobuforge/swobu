// Provider panel views for routing section.
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

// BuildProvidersCreatePanel shows provider setup in create mode.
func BuildProvidersCreatePanel(ctx *view.Context[state.Model]) view.ViewSpec[state.Model] {
	model := ctx.Model()
	configured := 0
	draftProvider := selectors.CreateDraftProviderConfig(model)
	if draftProvider != nil {
		configured = 1
	}
	open, setOpen := view.UseState(ctx, func() bool { return false })
	expanded, setExpanded := view.UseState(ctx, func() bool { return false })
	picker, setPicker := view.UseState(ctx, func() views.FilterablePickerState { return views.DefaultFilterablePickerState() })
	var cancelFn func() []update.Action
	if open || expanded {
		cancelFn = func() []update.Action {
			setOpen(false)
			setExpanded(false)
			return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeNAV}}
		}
	}
	parent := views.RowManageWithHooks(views.RowProviders, fmt.Sprintf("%d configured", configured), func() []update.Action {
		if open {
			setOpen(false)
			setExpanded(false)
			return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeNAV}}
		}
		setOpen(true)
		views.ResetFilterablePickerState(setPicker)
		return []update.Action{
			state.SetInteractionMode{Mode: state.InteractionModeManageList},
			interaction.FocusKeyAction{Key: views.FilterablePickerFocusKey("providers-create-option", 0)},
		}
	}, cancelFn, views.FocusAffordance("manage", false))
	var out view.ViewSpec[state.Model]
	if !open {
		out = parent
	} else {
		onClose := func() []update.Action {
			setOpen(false)
			setExpanded(false)
			return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeNAV}}
		}
		if draftProvider == nil {
			out = views.RenderFilterablePickerDisclosure(ctx, parent, picker, setPicker, createProviderSpecItems(model, onClose), views.FilterablePickerConfig{
				KeyPrefix:      "providers-create-option",
				BuildOptionRow: views.ChoicePickerOptionRow(true),
				WindowSize:     6,
				FindLabel:      "find",
				ShowSelected:   true,
				OnNoMatchFocus: func() []update.Action { return []update.Action{interaction.FocusKeyAction{Key: "providers"}} },
				OnCancel: func() []update.Action {
					setOpen(false)
					return []update.Action{
						state.SetInteractionMode{Mode: state.InteractionModeNAV},
						interaction.FocusKeyAction{Key: "providers"},
					}
				},
			})
		} else {
			providerRow := newProviderSummaryRow(
				*draftProvider,
				true,
				expanded,
				func() []update.Action {
					setExpanded(!expanded)
					return nil
				},
				onClose,
			)
			rows := []view.ViewSpec[state.Model]{providerRow}
			if expanded {
				rows = append(rows, createProviderPropertyRows("", nil, draftProvider, true)...)
			}
			out = toolkitviews.NewAnchoredDisclosure(parent, rows...)
		}
	}
	return out
}

// BuildProvidersWorkspacePanel shows provider setup in workspace mode.
func BuildProvidersWorkspacePanel(ctx *view.Context[state.Model]) view.ViewSpec[state.Model] {
	model := ctx.Model()
	snapshot := selectors.CurrentEndpointSnapshot(model)
	if snapshot == nil {
		return views.RowStatic("", "not configured")
	}
	return buildProvidersWorkspaceConfiguredPanel(ctx, model, snapshot)
}

func createProviderSpecItems(model state.Model, onCancel func() []update.Action) []views.FilterablePickerItem {
	options := state.ProviderOptions()
	if len(options) == 0 {
		return nil
	}
	currentSpec := ""
	if pc := selectors.CreateDraftProviderConfig(model); pc != nil {
		currentSpec = strings.TrimSpace(pc.ProviderSpec)
	}
	items := make([]views.FilterablePickerItem, 0, len(options))
	for _, option := range options {
		spec := strings.TrimSpace(option.Spec)
		label := providercatalog.DisplayName(spec)
		if strings.TrimSpace(label) == "" || strings.EqualFold(label, "Provider") {
			label = selectors.EmptyOr(strings.TrimSpace(option.Label), spec)
		}
		choiceSpec := spec
		items = append(items, views.FilterablePickerItem{
			Label:    label,
			Search:   choiceSpec + " " + label,
			Selected: choiceSpec == currentSpec,
			OnChoose: func() []update.Action {
				actions := []update.Action{state.SetCreateDraftProviderSpec{ProviderSpec: choiceSpec}}
				if onCancel != nil {
					actions = append(actions, onCancel()...)
				}
				return actions
			},
		})
	}
	return items
}

func createProviderPropertyRows(endpointName string, catalog *state.CatalogEntry, providerConfig *state.ProviderConfigSnapshot, createMode bool) []view.ViewSpec[state.Model] {
	rows := []view.ViewSpec[state.Model]{
		view.Named[state.Model]("provider", providerSpecRow(providerConfig)),
		view.Named[state.Model]("model", providerModelChoiceRow(providerModelChoiceRowSpec{
			Catalog:        catalog,
			ProviderConfig: providerConfig,
			EndpointName:   endpointName,
			CreateMode:     createMode,
		})),
		view.Named[state.Model]("alias", providerTargetAliasRow(providerTargetAliasRowSpec{
			ProviderConfig: providerConfig,
			EndpointName:   endpointName,
			CreateMode:     createMode,
		})),
	}
	if providerConfig != nil {
		if providerCredentialSelectionRequired(providerConfig.ProviderSpec, providerConfig.BaseURL, providerConfig.CredentialRef) {
			rows = append(rows, view.Named[state.Model]("credential", providerCredentialChoiceRow(providerCredentialChoiceRowSpec{
				ProviderConfig: providerConfig,
				EndpointName:   endpointName,
				CreateMode:     createMode,
			})))
		}
	}
	if providerConfig != nil && strings.EqualFold(credentialSource(providerConfig.CredentialRef), "keychain") {
		rows = append(rows, view.Named[state.Model]("key-name", providerKeychainKeyNameRow(providerKeychainKeyNameRowSpec{
			ProviderConfig: providerConfig,
			EndpointName:   endpointName,
			CreateMode:     createMode,
		})))
	}
	if providerConfig != nil && strings.EqualFold(credentialSource(providerConfig.CredentialRef), "env") {
		rows = append(rows, view.Named[state.Model]("env-key", providerEnvKeyRow(providerEnvKeyRowSpec{
			ProviderConfig: providerConfig,
			EndpointName:   endpointName,
			CreateMode:     createMode,
		})))
	}
	if providerConfig != nil && strings.EqualFold(credentialSource(providerConfig.CredentialRef), "file") {
		rows = append(rows, view.Named[state.Model]("credential-file", providerCredentialFileBrowseRow(providerCredentialFileBrowseRowSpec{
			ProviderConfig: providerConfig,
			EndpointName:   endpointName,
			CreateMode:     createMode,
		})))
	}
	if providerConfig != nil && strings.TrimSpace(providerConfig.ProviderSpec) == "custom" {
		rows = append(rows, view.Named[state.Model]("backend-url", providerBackendURLRow(providerBackendURLRowSpec{
			ProviderConfig: providerConfig,
			EndpointName:   endpointName,
			CreateMode:     createMode,
		})))
	}
	if !createMode && providerConfig != nil {
		rows = append(rows,
			view.Named[state.Model]("delete", providerDeleteRow(endpointName, providerConfig)),
		)
	}
	return rows
}

func newProviderSummaryRow(provider state.ProviderConfigSnapshot, selected, expanded bool, onActivate func() []update.Action, onCancel func() []update.Action) view.ViewSpec[state.Model] {
	return view.Build[state.Model](func(ctx *view.Context[state.Model]) view.ViewSpec[state.Model] {
		return providerSummaryRow(ctx, provider, selected, expanded, onActivate, onCancel)
	})
}

func providerSummaryRow(
	_ *view.Context[state.Model],
	provider state.ProviderConfigSnapshot,
	_ bool,
	expanded bool,
	onActivate func() []update.Action,
	onCancel func() []update.Action,
) view.ViewSpec[state.Model] {
	label := providerDisplayLabel(provider)
	verb := "edit"
	if expanded {
		verb = "close"
	}
	return views.RowActionWithCancel(label, "", verb, onActivate, onCancel)
}

func providerDisplayLabel(pc state.ProviderConfigSnapshot) string {
	alias := strings.TrimSpace(pc.TargetAlias)
	if alias != "" {
		return alias
	}
	providerSpec := strings.TrimSpace(pc.ProviderSpec)
	modelID := strings.TrimSpace(pc.ModelID)
	if modelID != "" {
		return modelID
	}
	return providercatalog.DisplayName(providerSpec)
}

func catalogEntryForProvider(model state.Model, endpointName, providerRef string) *state.CatalogEntry {
	endpointName = strings.TrimSpace(endpointName)
	providerRef = strings.TrimSpace(providerRef)
	for i := range model.Catalog {
		entry := model.Catalog[i]
		if strings.TrimSpace(entry.EndpointName) == endpointName && strings.TrimSpace(entry.ProviderConfigRef) == providerRef {
			return &model.Catalog[i]
		}
	}
	return nil
}

func providerRowKey(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "default"
	}
	return ref
}
