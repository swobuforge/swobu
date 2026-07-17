package target_config

import (
	"strings"

	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/profile"
)

// SelectProviderAuthMode commits one concrete Bedrock auth path. The UI keeps
// this flat because operators choose a usable auth source, not an abstract auth
// family followed by another prompt.
func (w *TargetConfig) SelectProviderAuthMode(mode string) {
	mode = strings.TrimSpace(mode)                        // swobu:io-string source=boundary
	spec := strings.TrimSpace(w.Draft.Get().ProviderSpec) // swobu:io-string source=boundary
	if profile.ProviderID(spec) != profile.ProviderSpecBedrock || mode == "" {
		return
	}
	requiresCredential, ok := profile.AuthModeRequiresCredentialForSpec(spec, profile.AuthMode(mode), strings.TrimSpace(w.BaseURL.Get()))
	if !ok {
		w.Error.Set("unsupported auth mode " + mode)
		w.Phase.Set(PhaseConfiguring)
		return
	}
	authMode := profile.AuthMode(mode)
	d := w.Draft.Get()
	d.ProviderOptions.Bedrock.AuthMode = mode
	switch authMode {
	case profile.AuthModeAWSProfile:
		if profile.BedrockProfileNameFromCredentialRef(d.CredentialRef) == "" {
			d.CredentialRef = ""
			d.ProviderOptions.Bedrock.ProfileName = ""
		}
	case profile.AuthModeAWSEnvSession:
		d.ProviderOptions.Bedrock.ProfileName = ""
		d.CredentialRef = ""
	default:
		d.ProviderOptions.Bedrock.ProfileName = ""
		if profile.BedrockProfileNameFromCredentialRef(d.CredentialRef) != "" {
			d.CredentialRef = ""
		}
	}
	if !requiresCredential && authMode != profile.AuthModeAWSProfile && authMode != profile.AuthModeAWSEnvSession {
		d.CredentialRef = ""
	}
	w.Draft.Set(d)
	w.Error.Set("")
	w.advanceFromSetup()
}

// SelectBedrockProfile commits one AWS profile name for Bedrock Mantle and
// resumes setup probing.
func (w *TargetConfig) SelectBedrockProfile(raw string) {
	profileName := profile.BedrockProfileNameFromInput(raw)
	if profileName == "" {
		w.Error.Set("profile first")
		return
	}
	d := w.Draft.Get()
	d.ProviderOptions.Bedrock.AuthMode = string(profile.AuthModeAWSProfile)
	d.ProviderOptions.Bedrock.ProfileName = profileName
	d.CredentialRef = profile.BedrockProfileCredentialRef(profileName)
	w.Draft.Set(d)
	w.Error.Set("")
	w.advanceFromSetup()
	w.CommitEdit(w.actionContext())
}

// SelectBedrockRegion commits one Mantle region: it owns its arm, so it writes
// Region directly and derives the canonical endpoint from it.
func (w *TargetConfig) SelectBedrockRegion(region string) {
	region = strings.TrimSpace(region)
	if region == "" || !profile.SupportsBedrockMantleRegion(region) {
		return
	}
	w.BaseURL.Set(profile.BedrockMantleEndpointForRegion(region))
	w.Draft.Update(func(d endpointintent.TargetDraft) endpointintent.TargetDraft {
		d.ProviderOptions.Bedrock.Region = region
		return d
	})
	w.Error.Set("")
	w.advanceFromSetup()
}

// BedrockRegionPicker is the Bedrock Mantle region picker, built fresh per
// render as the bedrock-region ui.Select body. Selecting a region commits and
// backs out.
func BedrockRegionPicker(w *TargetConfig, backout func()) *ui.SearchPicker {
	regions := profile.BedrockMantleRegions()
	opts := make([]ui.SearchOption, len(regions))
	for i, region := range regions {
		keywords := append([]string{region.ID}, region.Keywords...)
		opts[i] = ui.SearchOption{
			ID:       region.ID,
			Label:    region.Label,
			Keywords: keywords,
		}
	}
	picker := ui.NewSearchPicker(
		TargetAddMountKey(w, "bedrock-region-picker"),
		"region",
		opts,
		func(sel ui.Selection) {
			w.SelectBedrockRegion(sel.Value)
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

// BedrockProfilePicker is the AWS shared-config profile picker for the Bedrock
// Mantle flow, built fresh per render as the bedrock-profile ui.Select body.
func BedrockProfilePicker(w *TargetConfig, backout func()) *ui.SearchPicker {
	names := profile.AWSSharedConfigProfileNames()
	opts := make([]ui.SearchOption, len(names))
	for i, name := range names {
		opts[i] = ui.SearchOption{
			ID:       name,
			Label:    name,
			Keywords: []string{"aws", "profile", name},
		}
	}
	picker := ui.NewSearchPicker(
		TargetAddMountKey(w, "bedrock-profile-picker"),
		"profile",
		opts,
		func(sel ui.Selection) {
			w.SelectBedrockProfile(sel.Value)
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
