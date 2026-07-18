package target_config

import (
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/profile"
)

type customProviderForm struct{ target *TargetConfig }

func CustomProviderForm(w *TargetConfig) tui.Component {
	return &customProviderForm{target: w}
}

// customReadiness needs an explicit backend URL and, where required, a credential.
func customReadiness(w *TargetConfig, base providerSetupState) providerSetupState {
	setup := base
	endpointValue := strings.TrimSpace(w.BaseURL.Get()) // swobu:io-string source=boundary
	if endpointValue == "" {
		setup.Status = setupMissingLocator
		return setup
	}
	if setup.CredentialRequired && strings.TrimSpace(setup.CredentialRef) == "" {
		setup.Status = setupMissingCredential
		return setup
	}
	setup.Status = setupReady
	return setup
}

func (w *TargetConfig) IsCustomFlow() bool {
	return profile.ProviderID(w.Draft.Get().ProviderSpec) == profile.ProviderSpecCustom
}

// ShouldRenderCredentialHeaderRow shows the credential-header row only once a
// backend URL exists. The header names where the selected credential is placed,
// so it is meaningless while credential = none (RFC: Credential Semantics).
func shouldRenderCredentialHeaderRow(w *TargetConfig) bool {
	return w.IsCustomFlow() &&
		strings.TrimSpace(w.BaseURL.Get()) != "" &&
		strings.TrimSpace(w.Draft.Get().CredentialRef) != ""
}

func shouldRenderCustomCredentialRow(w *TargetConfig) bool {
	return w.IsCustomFlow() && strings.TrimSpace(w.BaseURL.Get()) != ""
}

// CredentialHeaderControl is the credential-header row as a ui.Select: the
// committed (or inferred) header value, with the header picker as its entered
// body. ui.Select owns the entered state; selecting a header commits and backs
// out through the shared ui.Select entered-state contract.
func CredentialHeaderControl(w *TargetConfig) *ui.Select {
	value := strings.TrimSpace(w.Draft.Get().CredentialHeader)
	if value == "" {
		value = w.inferredCredentialHeader()
	}
	return ui.NewSelect(ui.SelectProps{
		ID:     TargetAddMountKey(w, "credential-header"),
		Label:  "credential header",
		Value:  value,
		Action: "change ↵",
		Body: func(backout func()) tui.Component {
			return CredentialHeaderPicker(w, backout)
		},
	})
}

func (w *TargetConfig) commitCredentialHeader(header string) {
	d := w.Draft.Get()
	d.CredentialHeader = header
	w.Draft.Set(d)
	if w.CredentialHeaderEdited != nil {
		w.CredentialHeaderEdited.Set(true)
	}
}

// inferredCredentialHeader projects the default header for the current custom
// endpoint. The operator can still override this value explicitly.
func (w *TargetConfig) inferredCredentialHeader() string {
	return profile.InferredCredentialHeaderForBackendURL(w.BaseURL.Get())
}

// reseedInferredCredentialHeader refreshes the inferred header after the backend
// URL changes, without clobbering an operator's explicit choice.
func (w *TargetConfig) reseedInferredCredentialHeader() {
	if w.CredentialHeaderEdited != nil && w.CredentialHeaderEdited.Get() {
		return
	}
	d := w.Draft.Get()
	d.CredentialHeader = w.inferredCredentialHeader()
	w.Draft.Set(d)
}

// resolvedCredentialHeader returns the effective header for a custom endpoint:
// the materialized inferred value or the operator's explicit choice. The domain
// boundary repeats the default only as a defensive construction invariant.
func resolvedCredentialHeader(spec, header string) string {
	if h := strings.TrimSpace(header); h != "" {
		return h
	}
	if profile.ProviderID(spec) != profile.ProviderSpecCustom {
		return ""
	}
	if defaults := profile.SupportedAuthHeadersForSpec(spec); len(defaults) > 0 {
		return defaults[0]
	}
	return "Authorization"
}

// CredentialHeaderPicker is the open-set picker for credential-header names,
// built fresh per render as the credential-header ui.Select body. Common headers
// are candidates; the typed query is itself usable as a free-form header.
func CredentialHeaderPicker(w *TargetConfig, backout func()) *ui.SearchPicker {
	common := profile.SupportedAuthHeadersForSpec(w.Draft.Get().ProviderSpec)
	opts := make([]ui.SearchOption, 0, len(common))
	for _, header := range common {
		opts = append(opts, ui.SearchOption{
			ID:       header,
			Label:    header,
			Keywords: []string{header, strings.ToLower(header)},
		})
	}
	picker := ui.NewSearchPicker(
		TargetAddMountKey(w, "credential-header-picker"),
		"credential header",
		opts,
		func(sel ui.Selection) {
			w.commitCredentialHeader(strings.TrimSpace(sel.Value))
			if backout != nil {
				backout()
			}
		},
		func() {
			if backout != nil {
				backout()
			}
		},
	)
	picker.Mode = ui.SearchPickerOpen
	picker.AutoFocus = true
	return picker
}

templ (f *customProviderForm) Render() {
	<div class="flex-col w-full" deps={f.target.Draft, f.target.BaseURL}>
		if shouldRenderEndpointRow(f.target) { @EndpointInput(f.target, setupRequiresLocator(f.target)) }
		if shouldRenderCustomCredentialRow(f.target) { <div key={credentialRegionKey(f.target)} class="w-full">@CredentialControlRegion(f.target, setupRequiresCredential(f.target))</div> }
		if shouldRenderCredentialHeaderRow(f.target) { @CredentialHeaderControl(f.target) }
	</div>
}
