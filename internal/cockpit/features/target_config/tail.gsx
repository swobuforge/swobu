package target_config

import (
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/profile"
)

func TargetModelLabel(w *TargetConfig) string {
	return profile.CatalogItemLabelForSpec(w.Draft.Get().ProviderSpec)
}

func setupAllowsModelChoice(w *TargetConfig) bool {
	if w.RequiresInteractiveAuth() && strings.TrimSpace(w.Draft.Get().CredentialRef) == "" {
		return false
	}
	if w.IsCustomFlow() {
		return w.setupState().Ready() && !w.catalogLoading()
	}
	return w.catalogValidated()
}

func ModelPicker(w *TargetConfig, backout func()) *ui.SearchPicker {
	catalog := w.catalogResult()
	opts := make([]ui.SearchOption, 0, len(catalog.Deployments))
	for _, deployment := range catalog.Deployments {
		opts = append(opts, ui.SearchOption{ID: deployment.ID, Label: deployment.Name})
	}
	picker := ui.NewSearchPicker(TargetAddMountKey(w, "model-picker"), TargetModelLabel(w), opts, func(sel ui.Selection) {
		w.selectModelByID(sel.Value)
		if backout != nil {
			backout()
		}
	}, func() {
		if backout != nil {
			backout()
		}
	})
	picker.AutoFocus = true
	if w.IsCustomFlow() { picker.Mode = ui.SearchPickerOpen }
	return picker
}

func ModelCatalogRetry(w *TargetConfig) *ui.SelectableRow {
	value := "catalog failed"
	switch strings.ToLower(strings.TrimSpace(w.Catalog.Get().Err)) {
	case "project not found":
		value = "project not found"
	case "unauthorized":
		value = "unauthorized"
	case "none found":
		value = "none found"
	}
	row := ui.NewSelectableRow(TargetAddMountKey(w, "catalog-retry"), TargetModelLabel(w), value, "retry ↵", w.RetryCatalog)
	row.AutoFocus = true
	return row
}

func ModelCatalogLoading(w *TargetConfig) *ui.SelectableRow {
	row := ui.NewSelectableRow(TargetAddMountKey(w, "loading-catalog"), TargetModelLabel(w), "loading catalog…", "wait", nil)
	row.AutoFocus = true
	return row
}

func ModelSelectRow(w *TargetConfig) *ui.Select {
	model := strings.TrimSpace(w.SelectedModel.Get().ModelName)
	value, action := model, "change ↵"
	if value == "" {
		value, action = "required", "choose ↵"
	}
	return ui.NewSelect(ui.SelectProps{ID: TargetAddMountKey(w, "model-display"), Label: TargetModelLabel(w), Value: value, Action: action, AutoFocus: model == "" && setupAllowsModelChoice(w), CanEnter: func() bool { return setupAllowsModelChoice(w) }, Body: func(backout func()) tui.Component { return ModelPicker(w, backout) }})
}

func protocolDisplayLabel(w *TargetConfig, protocol string) string {
	for _, option := range w.CurrentProtocolOptions() {
		if option.ID == protocol {
			return option.Label
		}
	}
	return protocol
}
func SetupAllowsProtocolChoice(w *TargetConfig) bool {
	return strings.TrimSpace(w.SelectedModel.Get().ModelName) != "" && len(w.CurrentProtocolOptions()) > 0
}

func fixedProviderProtocol(w *TargetConfig) (string, bool) {
	protocols := profile.ConcreteProviderProtocolsForSpec(w.Draft.Get().ProviderSpec)
	if len(protocols) != 1 { return "", false }
	return protocolOptionLabel(protocols[0]), true
}

func ProtocolPicker(w *TargetConfig, backout func()) *ui.SearchPicker {
	options := w.CurrentProtocolOptions()
	opts := make([]ui.SearchOption, len(options))
	for i, option := range options {
		opts[i] = ui.SearchOption{ID: option.ID, Label: option.Label, Keywords: option.Keywords}
	}
	picker := ui.NewSearchPicker(TargetAddMountKey(w, "protocol-picker"), "protocol", opts, func(sel ui.Selection) {
		w.selectProtocol(sel.Value)
		if backout != nil {
			backout()
		}
	}, func() {
		if backout != nil {
			backout()
		}
	})
	picker.AutoFocus = true
	return picker
}

func ProtocolSelect(w *TargetConfig) *ui.Select {
	protocol := strings.TrimSpace(w.Draft.Get().ProviderProtocol)
	options := w.CurrentProtocolOptions()
	modelSelected := strings.TrimSpace(w.SelectedModel.Get().ModelName) != ""
	required := modelSelected && protocol == "" && len(options) > 1
	value, action, enterable := protocol, "change ↵", true
	switch {
	case protocol == "":
		value, action = "required", "choose ↵"
	case len(options) == 1:
		value, action, enterable = protocolDisplayLabel(w, protocol), "fixed", false
	default:
		value = protocolDisplayLabel(w, protocol)
	}
	props := ui.SelectProps{ID: TargetAddMountKey(w, "protocol-display"), Label: "protocol", Value: value, Action: action, AutoFocus: required, CanEnter: func() bool { return enterable }}
	if enterable {
		props.Body = func(backout func()) tui.Component { return ProtocolPicker(w, backout) }
	}
	return ui.NewSelect(props)
}

func PlacementSelect(w *TargetConfig) *ui.Select {
	return ui.NewSelect(ui.SelectProps{ID: TargetAddMountKey(w, "placement-display"), Label: "routing", Value: w.Placement.Get().Summary(), Action: "change ↵", CanEnter: w.readyToCreate, Body: func(backout func()) tui.Component { return PlacementPicker(w, backout) }})
}
func PlacementPicker(w *TargetConfig, backout func()) *ui.SearchPicker {
	opts := placementOptions(w.Route, w.mode, w.Target.ID)
	items := make([]ui.SearchOption, 0, len(opts))
	for _, opt := range opts {
		items = append(items, ui.SearchOption{ID: placementOptionID(opt), Label: opt.Summary()})
	}
	picker := ui.NewSearchPicker(TargetAddMountKey(w, "placement-picker"), "routing", items, func(sel ui.Selection) {
		for _, opt := range opts {
			if placementOptionID(opt) == sel.Value {
				w.SelectPlacement(opt)
				break
			}
		}
		if backout != nil {
			backout()
		}
	}, func() {
		if backout != nil {
			backout()
		}
	})
	picker.AutoFocus = true
	return picker
}
func canChangePlacement(w *TargetConfig) bool {
	return w.mode == targetConfigModeCreate && w.Route.TargetCount() > 0
}

func TargetConfigHeader(w *TargetConfig) *ui.SelectableRow {
	return ui.NewSelectableRow(TargetAddMountKey(w, "target-config-parent"), "", targetConfigTitle(w), targetConfigParentAction(w), w.Close)
}

func CreateRetryControl(w *TargetConfig) *ui.SelectableRow {
	return ui.NewSelectableRow(TargetAddMountKey(w, "create-retry"), w.saveVerb(), "failed", "retry ↵", w.RetryCreate)
}

func DeleteControl(w *TargetConfig) *ui.SelectableRow {
	value, action := "target", "delete ↵"
	activate := func() { w.DeleteArmed.Set(true) }
	if w.DeleteArmed.Get() {
		value, action = "delete target?", "confirm ↵"
		activate = func() {
			if w.OnDeleteConfirmed == nil {
				w.Error.Set("target delete is not wired yet")
				return
			}
			if err := w.OnDeleteConfirmed(); err != nil {
				w.Error.Set(err.Error())
				return
			}
			w.Error.Set("")
			w.DeleteArmed.Set(false)
		}
	}
	return ui.NewSelectableRow(TargetAddMountKey(w, "delete"), "delete", value, action, activate)
}

func CreateControl(w *TargetConfig) *ui.SelectableRow {
	row := ui.NewSelectableRow(TargetAddMountKey(w, "create"), w.saveVerb(), "", w.saveVerb()+" ↵", func() { w.Create(w.actionContext()) })
	row.AutoFocus = true
	return row
}

func (w *TargetConfig) saveVerb() string {
	if w.mode == targetConfigModeEdit {
		return "save"
	}
	return "create"
}

// targetTail is the provider-form tail component. Its visible hierarchy lives
// in this GSX source.
type targetTail struct{ root *TargetConfig }

func TargetConfigTail(w *TargetConfig) tui.Component { return &targetTail{root: w} }

templ InertTargetField(label string, value string, action string) {
	<div class="flex w-full">
		<span class="w-2"> </span>
		<span class="w-18">{label}</span>
		<span class="flex-1">{value}</span>
		if action != "" { <span class="w-14">{action}</span> }
	</div>
}


templ (t *targetTail) Render() {
	<div class="flex-col w-full">
		if targetCatalogLoading(t.root) {
			@ModelCatalogLoading(t.root)
		} else if t.root.IsCustomFlow() && setupAllowsModelChoice(t.root) {
			@ModelSelectRow(t.root)
		} else if targetCatalogFailed(t.root) && t.root.IsBedrockFlow() {
			@InertTargetField(TargetModelLabel(t.root), "waiting for setup", "")
		} else if targetCatalogFailed(t.root) {
			@ModelCatalogRetry(t.root)
		} else if setupAllowsModelChoice(t.root) {
			@ModelSelectRow(t.root)
		} else {
			@InertTargetField(TargetModelLabel(t.root), "waiting for setup", "")
		}

		if SetupAllowsProtocolChoice(t.root) {
			@ProtocolSelect(t.root)
		} else if label, fixed := fixedProviderProtocol(t.root); fixed {
			@InertTargetField("protocol", label, "fixed")
		} else {
			@InertTargetField("protocol", "waiting for "+TargetModelLabel(t.root), "")
		}

		if t.root.mode != targetConfigModeEdit {
			if canChangePlacement(t.root) {
				@PlacementSelect(t.root)
			}
		}

		if t.root.mode == targetConfigModeEdit {
			@DeleteControl(t.root)
		} else if targetCreateFailed(t.root) {
			@CreateRetryControl(t.root)
		} else if targetReadyToCreate(t.root) {
			@CreateControl(t.root)
		} else {
			@InertTargetField(targetSaveVerb(t.root), "", "complete setup")
		}
	</div>
}
