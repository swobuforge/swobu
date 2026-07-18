package target_config

import (
	"strings"

	"github.com/swobuforge/swobu/internal/cockpit/readmodel"
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
	}, func() {
		w.Back()
	})
	picker.AutoFocus = true
	return picker
}

func providerPickerLabel(providerSpec, displayName string) string {
	if label := strings.TrimSpace(displayName); label != "" {
		return label
	}
	return strings.TrimSpace(providerSpec)
}

func providerPickerKeywords(p readmodel.ProviderOptionReadModel) []string {
	keywords := make([]string, 0, 4)
	if spec := strings.TrimSpace(p.ProviderSpec); spec != "" {
		keywords = append(keywords, spec)
	}
	if hint := strings.TrimSpace(p.SetupHint); hint != "" {
		keywords = append(keywords, hint)
	}
	if summary := strings.TrimSpace(profile.ProviderSetupKeywordSummaryForSpec(p.ProviderSpec)); summary != "" {
		keywords = append(keywords, summary)
	}
	return keywords
}

func (w *TargetConfig) SelectProvider(spec string) {
	spec = strings.TrimSpace(spec) // swobu:io-string source=boundary
	w.resetFlowState()
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft { d.ProviderSpec = spec; return d })
	w.Error.Set("")
	w.seedSetupDefaults()
	seedProviderDefaults(w)
	w.refreshSetup()
	w.Phase.Set(PhaseConfiguring)
}

func (w *TargetConfig) SetSetupReady(credentialRef, baseURL string) {
	credentialRef, baseURL = strings.TrimSpace(credentialRef), strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = profile.DefaultExecuteBaseURL(w.Draft.Get().ProviderSpec)
	}
	w.Draft.Update(func(d readmodel.TargetDraft) readmodel.TargetDraft {
		d.CredentialRef = credentialRef
		return d
	})
	w.BaseURL.Set(baseURL)
	w.Error.Set("")
	w.advanceFromSetup()
}

func seedProviderDefaults(w *TargetConfig) {
	if profile.ProviderID(w.Draft.Get().ProviderSpec) == profile.ProviderSpecOpenAICompatible {
		w.reseedInferredCredentialHeader()
	}
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

func (w *TargetConfig) ShouldRenderEndpointRow() bool {
	setup := w.setupState()
	return setup.EndpointLabel != "" && profile.RequiresExplicitEndpoint(w.Draft.Get().ProviderSpec) && (strings.TrimSpace(w.BaseURL.Get()) != "" || setup.RequiresEndpoint)
}

func ProviderSummary(w *TargetConfig) *ui.SelectableRow {
	action := "change ↵"
	activate := func() { w.ChangeProvider() }
	if w.mode == targetConfigModeEdit {
		action, activate = "fixed", nil
	}
	return ui.NewSelectableRow(
		TargetAddMountKey(w, "provider-display"), "provider", providerDisplay(w), action, activate,
	)
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
		w.reseedInferredCredentialHeader()
		w.advanceFromSetup()
	}
	row.CloseAfterSubmit = func() bool { return !w.setupState().RequiresEndpoint }
	return row
}
