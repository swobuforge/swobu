package target_config

import (
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/profile"
)

type openAICompatibleProviderForm struct{ target *TargetConfig }

func OpenAICompatibleProviderForm(w *TargetConfig) tui.Component { return &openAICompatibleProviderForm{target: w} }

// openAICompatibleReadiness needs an explicit endpoint and, where required, a credential.
func openAICompatibleReadiness(w *TargetConfig, base providerSetupState) providerSetupState {
	setup := base
	endpointValue := strings.TrimSpace(w.BaseURL.Get()) // swobu:io-string source=boundary
	setup.RequiresEndpoint = endpointValue == ""
	if setup.RequiresEndpoint {
		setup.CredentialLabel = "enter " + setup.EndpointLabel
		setup.BlockReason = setup.CredentialLabel
		return setup
	}
	if setup.CredentialRequired && strings.TrimSpace(setup.CredentialRef) == "" {
		setup.CredentialLabel = "enter credential"
		setup.BlockReason = setup.CredentialLabel
		return setup
	}
	if setup.CredentialRef != "" {
		setup.CredentialLabel = setup.CredentialRef
	} else {
		setup.CredentialLabel = "none"
	}
	setup.ReadyForCatalog = true
	return setup
}

func (w *TargetConfig) IsOpenAICompatibleFlow() bool {
	return profile.ProviderID(w.Draft.Get().ProviderSpec) == profile.ProviderSpecOpenAICompatible
}

// ShouldRenderCredentialHeaderRow shows the credential-header row only once a
// backend URL exists. The header names where the selected credential is placed,
// so it is meaningless while credential = none (RFC: Credential Semantics).
func (w *TargetConfig) ShouldRenderCredentialHeaderRow() bool {
	if !w.IsOpenAICompatibleFlow() || strings.TrimSpace(w.BaseURL.Get()) == "" {
		return false
	}
	return !w.CredentialIsNone()
}

func (w *TargetConfig) ShouldRenderOpenAICompatibleCredentialRow() bool {
	return genericCredentialRowVisible(w) || genericCredentialDisplayVisible(w)
}

// CredentialIsNone reports whether the operator committed the "no auth" path.
// The credential header is hidden in that case because there is no credential
// to place in any header.
func (w *TargetConfig) CredentialIsNone() bool {
	if strings.TrimSpace(w.Draft.Get().CredentialRef) != "" {
		return false
	}
	setup := w.setupState()
	return setup.ReadyForCatalog && strings.TrimSpace(setup.CredentialLabel) == "none"
}

// CredentialHeaderControl is the credential-header row as a ui.Select: the
// committed (or inferred) header value, with the header picker as its entered
// body. ui.Select owns the entered state; selecting a header commits and backs
// out (task 090: ui.Select replaces the legacy credentialHeaderControl).
func CredentialHeaderControl(w *TargetConfig) *ui.Select {
	value := strings.TrimSpace(w.Draft.Get().ProviderOptions.OpenAICompatible.CredentialHeader)
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
	d.ProviderOptions.OpenAICompatible.CredentialHeader = header
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
	d.ProviderOptions.OpenAICompatible.CredentialHeader = w.inferredCredentialHeader()
	w.Draft.Set(d)
}

// resolvedCredentialHeader returns the effective header for an openai-compatible
// provider: the operator's explicit choice, or the inferred default when the arm
// is empty. The domain boundary applies the same default, so the arm stays empty
// until the operator picks one.
func resolvedCredentialHeader(spec, header string) string {
	if h := strings.TrimSpace(header); h != "" {
		return h
	}
	if profile.ProviderID(spec) != profile.ProviderSpecOpenAICompatible {
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
