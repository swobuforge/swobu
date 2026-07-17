package target_config

import (
	"strings"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/profile"
)

type bedrockProviderForm struct{ target *TargetConfig }

func BedrockProviderForm(w *TargetConfig) tui.Component { return &bedrockProviderForm{target: w} }

// bedrockReadiness needs a region, an auth mode, and where required a credential.
func bedrockReadiness(w *TargetConfig, base providerSetupState) providerSetupState {
	setup := base
	region := profile.BedrockMantleRegionFromEndpoint(w.BaseURL.Get())
	endpointValue := profile.BedrockMantleEndpointForRegion(region)
	setup.RequiresEndpoint = region == ""
	if setup.RequiresEndpoint {
		setup.BlockReason = "region first"
		return setup
	}
	authMode := strings.TrimSpace(w.Draft.Get().ProviderOptions.Bedrock.AuthMode) // swobu:io-string source=boundary
	if authMode == "" {
		setup.AuthModeRequired = true
		setup.BlockReason = "auth first"
		return setup
	}
	if profile.AuthMode(authMode) == profile.AuthModeAWSProfile {
		profileName := profile.BedrockProfileNameFromCredentialRef(setup.CredentialRef)
		if profileName == "" {
			setup.CredentialLabel = "enter profile"
			setup.BlockReason = "profile first"
			return setup
		}
		setup.CredentialLabel = profileName
		setup.ReadyForCatalog = true
		return setup
	}
	credentialRequired, ok := profile.AuthModeRequiresCredentialForSpec(string(profile.ProviderSpecBedrock), profile.AuthMode(authMode), endpointValue)
	if !ok {
		setup.AuthModeRequired = true
		setup.BlockReason = "auth first"
		return setup
	}
	setup.CredentialRequired = credentialRequired
	if setup.CredentialRequired && strings.TrimSpace(setup.CredentialRef) == "" {
		setup.CredentialLabel = "enter credential"
		setup.BlockReason = "credential first"
		return setup
	}
	if setup.CredentialRef != "" {
		setup.CredentialLabel = setup.CredentialRef
	} else if profile.AuthMode(authMode) == profile.AuthModeAWSEnvSession {
		setup.CredentialLabel = "AWS env"
	}
	setup.ReadyForCatalog = true
	return setup
}

func (w *TargetConfig) IsBedrockFlow() bool {
	return profile.ProviderID(w.Draft.Get().ProviderSpec) == profile.ProviderSpecBedrock
}

func (w *TargetConfig) ShouldRenderBedrockRegionRow() bool {
	return w.IsBedrockFlow()
}

func (w *TargetConfig) ShouldRenderBedrockConnectionRow() bool {
	return w.IsBedrockFlow()
}

func (w *TargetConfig) ShouldRenderBedrockEndpointRow() bool {
	return w.IsBedrockFlow() &&
		profile.BedrockMantleRegionFromEndpoint(w.BaseURL.Get()) != ""
}

func (w *TargetConfig) ShouldRenderBedrockAuthModeRow() bool {
	return w.IsBedrockFlow() &&
		profile.BedrockMantleRegionFromEndpoint(w.BaseURL.Get()) != ""
}

func (w *TargetConfig) ShouldRenderBedrockProfileRow() bool {
	return w.ShouldRenderBedrockAuthModeRow() &&
		strings.TrimSpace(w.Draft.Get().ProviderOptions.Bedrock.AuthMode) == string(profile.AuthModeAWSProfile)
}

func (w *TargetConfig) ShouldRenderBedrockCredentialRow() bool {
	switch profile.AuthMode(strings.TrimSpace(w.Draft.Get().ProviderOptions.Bedrock.AuthMode)) {
	case profile.AuthModeAWSProfile, profile.AuthModeAWSEnvSession:
		return false
	default:
		return genericCredentialRowVisible(w)
	}
}

// BedrockProfileControl is bedrock's AWS-profile row. It reads its own arm
// (Bedrock.ProfileName) — bedrock reading bedrock, not a parent leak.
func BedrockProfileControl(w *TargetConfig) *ui.Select {
	value := profile.BedrockProfileNameFromCredentialRef(w.Draft.Get().CredentialRef)
	if value == "" {
		value = profile.BedrockProfileNameFromInput(w.Draft.Get().ProviderOptions.Bedrock.ProfileName)
	}
	action := "change ↵"
	if value == "" {
		value = "required"
		action = "choose ↵"
	}
	return ui.NewSelect(ui.SelectProps{
		ID:        TargetAddMountKey(w, "bedrock-profile"),
		Label:     "profile",
		Value:     value,
		Action:    action,
		AutoFocus: profile.BedrockProfileNameFromCredentialRef(w.Draft.Get().CredentialRef) == "",
		Body:      func(backout func()) tui.Component { return BedrockProfilePicker(w, backout) },
	})
}

func BedrockRegionControl(w *TargetConfig) *ui.Select {
	region := profile.BedrockMantleRegionFromEndpoint(w.BaseURL.Get())
	value := region
	action := "change ↵"
	if region == "" {
		value = "required"
		action = "choose ↵"
	}
	return ui.NewSelect(ui.SelectProps{
		ID:        TargetAddMountKey(w, "bedrock-region"),
		Label:     "region",
		Value:     value,
		Action:    action,
		AutoFocus: w.setupState().RequiresEndpoint && region == "",
		Body:      func(backout func()) tui.Component { return BedrockRegionPicker(w, backout) },
	})
}

func BedrockConnectionControl(w *TargetConfig) *ui.SelectableRow {
	return ui.NewSelectableRow(
		TargetAddMountKey(w, "bedrock-connection"),
		"connection",
		"Mantle",
		"default",
		nil,
	)
}

func BedrockEndpointControl(w *TargetConfig) *ui.SelectableRow {
	region := profile.BedrockMantleRegionFromEndpoint(w.BaseURL.Get())
	endpoint := profile.BedrockMantleEndpointForRegion(region)
	return ui.NewSelectableRow(
		TargetAddMountKey(w, "bedrock-endpoint"),
		"endpoint",
		endpoint,
		"derived",
		nil,
	)
}

func BedrockAuthControl(w *TargetConfig) *ui.Select {
	value := providerAuthModeLabel(w.Draft.Get().ProviderSpec, w.Draft.Get().ProviderOptions.Bedrock.AuthMode)
	action := "change ↵"
	if value == "" {
		value = "required"
		action = "choose ↵"
	}
	return ui.NewSelect(ui.SelectProps{
		ID:     TargetAddMountKey(w, "bedrock-auth-mode"),
		Label:  "auth",
		Value:  value,
		Action: action,
		Body:   func(backout func()) tui.Component { return bedrockAuthModePicker(w, backout) },
	})
}

// bedrockAuthModePicker is the auth-mode SearchPicker, built fresh per render as
// the bedrock-auth ui.Select body. Selecting a mode commits and backs out.
func bedrockAuthModePicker(w *TargetConfig, backout func()) *ui.SearchPicker {
	opts := providerAuthModeOptions(w.Draft.Get().ProviderSpec)
	sopts := make([]ui.SearchOption, len(opts))
	for i, o := range opts {
		sopts[i] = ui.SearchOption{ID: o.ID, Label: o.Label, Keywords: []string{o.ID, o.Label}}
	}
	picker := ui.NewSearchPicker(
		TargetAddMountKey(w, "bedrock-auth-picker"),
		"auth",
		sopts,
		func(sel ui.Selection) {
			w.SelectProviderAuthMode(sel.Value)
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
	picker.AutoFocus = true
	return picker
}

type providerAuthModeOption struct {
	ID    string
	Label string
}

func providerAuthModeOptions(spec string) []providerAuthModeOption {
	if profile.ProviderID(spec) == profile.ProviderSpecBedrock {
		return []providerAuthModeOption{
			{ID: string(profile.AuthModeEnv), Label: "Bedrock API key"},
			{ID: string(profile.AuthModeAWSProfile), Label: "AWS profile"},
			{ID: string(profile.AuthModeAWSEnvSession), Label: "AWS env"},
		}
	}
	modes := profile.AllowedAuthModesForSpec(spec)
	options := make([]providerAuthModeOption, 0, len(modes))
	for _, mode := range modes {
		if strings.TrimSpace(string(mode.Mode)) == "" {
			continue
		}
		label := strings.TrimSpace(mode.Label)
		if label == "" {
			label = profile.AuthModeDisplayLabel(mode.Mode)
		}
		options = append(options, providerAuthModeOption{
			ID:    string(mode.Mode),
			Label: label,
		})
	}
	return options
}

func providerAuthModeLabel(spec string, mode string) string {
	normalized := strings.TrimSpace(mode) // swobu:io-string source=boundary
	for _, opt := range providerAuthModeOptions(spec) {
		if opt.ID == normalized {
			return opt.Label
		}
	}
	return ""
}
