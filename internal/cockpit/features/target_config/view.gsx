package target_config

import (
	"strings"

	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/profile"
)

func ProviderSelect(w *TargetConfig) *ui.SearchPicker {
	opts := make([]ui.SearchOption, len(w.providerOptions))
	for i, p := range w.providerOptions {
		opts[i] = ui.SearchOption{ID: p.ProviderSpec, Label: providerPickerLabel(p.ProviderSpec, p.DisplayName), Keywords: providerPickerKeywords(p)}
	}
	picker := ui.NewSearchPicker("provider-picker", "provider", opts, func(sel ui.Selection) {
		w.SelectProvider(sel.Value)
		w.ContinueSetup()
	}, func() { w.Back() })
	picker.AutoFocus = true
	return picker
}

func providerDisplay(w *TargetConfig) string {
	if setup := w.setupState(); setup.DisplayName != "" {
		return setup.DisplayName
	}
	spec := w.Draft.Get().ProviderSpec
	for _, option := range w.providerOptions {
		if option.ProviderSpec == spec {
			return option.DisplayName
		}
	}
	return spec
}

func ProviderSummary(w *TargetConfig) *ui.SelectableRow {
	action := "change ↵"
	activate := func() { w.ChangeProvider() }
	if w.mode == targetConfigModeEdit {
		action, activate = "fixed", nil
	}
	return ui.NewSelectableRow(TargetAddMountKey(w, "provider-display"), "provider", providerDisplay(w), action, activate)
}

func shouldRenderEndpointRow(w *TargetConfig) bool {
	setup := w.setupState()
	return setup.EndpointLabel != "" && setup.LocatorKind != profile.LocatorFixed
}

func EndpointInput(w *TargetConfig, autoFocus bool) *ui.EditableRow {
	label := strings.TrimSpace(w.setupState().EndpointLabel)
	if label == "" {
		label = "endpoint"
	}
	row := ui.NewEditableRow(TargetAddMountKey(w, "provider-setup-endpoint"), label, w.BaseURL)
	row.Placeholder, row.ViewAction, row.EditAction, row.AutoFocus = "enter "+label, "edit ↵", "save ↵", autoFocus
	row.OnSubmit = func(raw string) {
		w.BaseURL.Set(strings.TrimSpace(raw))
		w.invalidateCatalogSelection()
		w.reseedInferredCredentialHeader()
		w.advanceFromSetup()
	}
	row.CloseAfterSubmit = func() bool { return !w.setupState().RequiresLocator() }
	return row
}

templ (w *TargetConfig) Render() {
	<div class="flex-col w-full" deps={w.Lifecycle}>
		if w.IsOpen() {
			<div key={TargetAddMountKey(w, "target-config-parent")} class="w-full">
				@TargetConfigHeader(w)
			</div>
			<div class="pl-3 flex-col w-full" deps={w.Draft}>
				if strings.TrimSpace(w.Draft.Get().ProviderSpec) == "" {
					@ProviderSelect(w)
				} else {
					@ProviderSummary(w)
					if profile.ProviderID(w.Draft.Get().ProviderSpec) == profile.ProviderSpecAzure {
						@AzureProviderForm(w)
					} else if profile.ProviderID(w.Draft.Get().ProviderSpec) == profile.ProviderSpecCustom {
						@CustomProviderForm(w)
					} else if profile.ProviderID(w.Draft.Get().ProviderSpec) == profile.ProviderSpecBedrock {
						@BedrockProviderForm(w)
					} else if profile.ProviderID(w.Draft.Get().ProviderSpec) == profile.ProviderSpecChatGPT {
						@ChatGPTProviderForm(w)
					} else {
						@HTTPProviderForm(w)
					}
					if w.ShouldRenderTargetTail() { @TargetConfigTail(w) }
					@TargetConfigError(w)
				}
			</div>
		}
	</div>
}
