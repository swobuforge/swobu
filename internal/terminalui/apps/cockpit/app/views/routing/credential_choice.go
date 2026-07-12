// Provider credential choice row.
package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

type providerCredentialChoiceRowSpec struct {
	ProviderConfig *state.ProviderConfigSnapshot
	EndpointName   string
	CreateMode     bool
}

// credentialChoiceOption carries the canonical auth mode plus display data.
// Mode drives behavior; Label is presentation-only.
type credentialChoiceOption struct {
	Mode  profile.AuthMode
	Label string
	// FocusKey is retained-TUI plumbing, not credential semantics.
	FocusKey string
}

func providerCredentialChoiceRow(spec providerCredentialChoiceRowSpec) retained.ViewSpec[state.Model] {
	return retained.Build[state.Model](func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		return buildProviderCredentialChoiceRow(ctx, spec)
	})
}

func buildProviderCredentialChoiceRow(ctx *retained.Context[state.Model], spec providerCredentialChoiceRowSpec) retained.ViewSpec[state.Model] {
	pc := selectedProvider(ctx.Model(), spec.ProviderConfig, spec.CreateMode)
	currentRef := ""
	providerSpec := ""
	if pc != nil {
		currentRef = strings.TrimSpace(pc.CredentialRef)  // swobu:io-string source=boundary
		providerSpec = strings.TrimSpace(pc.ProviderSpec) // swobu:io-string source=boundary
	}
	currentChoice := credentialSource(currentRef)
	current := currentChoice
	if isResolvedInteractiveCredential(providerSpec, currentRef) {
		current = "signed in"
	}
	if strings.EqualFold(currentChoice, "keychain") && profile.SupportsAuthMode(providerSpec, profile.AuthModeKeychain) {
		current = authModeDisplayLabel(profile.AuthModeKeychain)
	}
	if current == "" {
		current = selectors.CredentialSummaryFromProviderConfig(pc)
	}
	open, setOpen := retained.UseState(ctx, func() bool { return false })
	picker, setPicker := retained.UseState(ctx, func() views.FilterablePickerState { return views.DefaultFilterablePickerState() })
	items := credentialOptionItems(currentChoice, func(choice credentialChoiceOption) []update.Action {
		setOpen(false)
		actions := applyProviderCredentialSelection(choice.Mode, providerSpec, spec.ProviderConfig, spec.EndpointName, spec.CreateMode)
		actions = append(actions, state.SetInteractionMode{Mode: state.InteractionModeManageList})
		if focusKey := strings.TrimSpace(choice.FocusKey); focusKey != "" {
			actions = append(actions, interaction.FocusKeyAction{Key: focusKey})
		}
		return actions
	}, providerSpec)
	parent := views.RowChoiceWithCancel(views.RowUseKeyFrom, current, func() []update.Action {
		nextOpen := !open
		setOpen(nextOpen)
		mode := state.InteractionModeManageList
		if nextOpen {
			views.ResetFilterablePickerState(setPicker)
			mode = state.InteractionModePickOne
		}
		actions := []update.Action{state.SetInteractionMode{Mode: mode}}
		if nextOpen {
			if focusKey := views.FilterablePickerFirstFocusKey(items, views.FilterablePickerConfig{KeyPrefix: "credential-source-option"}); focusKey != "" {
				actions = append(actions, interaction.FocusKeyAction{Key: focusKey})
			}
		}
		return actions
	}, func() []update.Action {
		if open {
			setOpen(false)
			return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeManageList}}
		}
		return nil
	})
	var out retained.ViewSpec[state.Model]
	if !open {
		out = parent
		if current == views.ValueRequired {
			out = toolkitviews.NewAnchoredDisclosure(parent, views.DisclosureNoteRows("authentication needed - choose a credential ref to save")...)
		}
	} else {
		out = views.RenderFilterablePickerDisclosure(ctx, parent, picker, setPicker, items, views.FilterablePickerConfig{
			KeyPrefix:      "credential-source-option",
			BuildOptionRow: views.ChoicePickerOptionRow(true),
			WindowSize:     6,
			NonFilterable:  true,
			OnCancel: func() []update.Action {
				setOpen(false)
				return []update.Action{
					state.SetInteractionMode{Mode: state.InteractionModeManageList},
					interaction.FocusKeyAction{Key: "credential"},
				}
			},
		})
	}
	return out
}

func applyProviderCredentialSelection(credentialRef profile.AuthMode, providerSpec string, providerConfig *state.ProviderConfigSnapshot, endpointName string, createMode bool) []update.Action {
	ref := strings.TrimSpace(string(credentialRef)) // swobu:io-string source=boundary
	providerSpec = strings.TrimSpace(providerSpec)  // swobu:io-string source=boundary
	endpointName = strings.TrimSpace(endpointName)  // swobu:io-string source=boundary
	mode := profile.AuthMode(strings.ToLower(ref))
	if profile.IsInteractiveAuthMode(mode) {
		if createMode {
			return []update.Action{state.SetCreateDraftCredentialRef{CredentialRef: ref}}
		}
		if providerConfig == nil || endpointName == "" {
			return nil
		}
		providerRef := ""
		if providerConfig != nil {
			providerRef = strings.TrimSpace(providerConfig.Ref) // swobu:io-string source=boundary
		}
		return []update.Action{state.StartProviderAuthSessionRequested{
			EndpointName:   endpointName,
			ProviderConfig: *providerConfig,
			OwnerKey:       stateModel.EndpointProviderAuthOwnerKey(endpointName, providerRef).String(),
			AuthScope:      stateModel.AuthScopeEndpointProvider,
		}}
	}
	if strings.EqualFold(ref, "env") {
		ref = encodeCredentialEnvRef(profile.DefaultEnvKeyForSpec(providerSpec))
	}
	if strings.EqualFold(ref, "file") {
		ref = encodeCredentialFileRef("")
	}
	if createMode {
		return []update.Action{state.SetCreateDraftCredentialRef{CredentialRef: ref}}
	}
	if providerConfig == nil || endpointName == "" {
		return nil
	}
	next := *providerConfig
	next.CredentialRef = ref
	return routingSaveProviderConfigActions(endpointName, next, "provider/auth")
}

func credentialOptionItems(
	current string,
	onChoose func(credentialChoiceOption) []update.Action,
	providerSpec string,
) []views.FilterablePickerItem {
	containsOptionValue := func(values []credentialChoiceOption, value string) bool {
		for _, item := range values {
			if strings.TrimSpace(string(item.Mode)) == strings.TrimSpace(value) { // swobu:io-string source=boundary
				return true
			}
		}
		return false
	}
	shouldExposeCurrentAsOption := func(providerSpec string, current string) bool {
		current = strings.TrimSpace(current)           // swobu:io-string source=boundary
		providerSpec = strings.TrimSpace(providerSpec) // swobu:io-string source=boundary
		if current == "" || current == views.ValueRequired || current == "signed in" {
			return false
		}
		if strings.EqualFold(providerSpec, "bedrock") && isBedrockAWSProfileCredentialRef(current) {
			// Bedrock credential picker is mode-only; profile selection is owned by
			// the dedicated profile row and must not leak encoded refs.
			return false
		}
		return true
	}
	descriptors := authModeDescriptorsForSpec(providerSpec)
	options := make([]credentialChoiceOption, 0, len(descriptors))
	for _, descriptor := range descriptors {
		if strings.EqualFold(strings.TrimSpace(providerSpec), "bedrock") && descriptor.Mode == profile.AuthModeAWSEnvSession { // swobu:io-string source=boundary
			// Bedrock exposes one canonical AWS chain affordance in UI.
			continue
		}
		label := descriptor.Label
		if strings.EqualFold(strings.TrimSpace(providerSpec), "bedrock") && descriptor.Mode == profile.AuthModeEnv { // swobu:io-string source=boundary
			label = "Bedrock API key"
		}
		focusKey := ""
		switch descriptor.Mode {
		case profile.AuthModeFile:
			focusKey = "credential-file"
		case profile.AuthModeEnv:
			focusKey = "env"
		case profile.AuthModeKeychain:
			focusKey = "keychain"
		}
		options = append(options, credentialChoiceOption{
			Mode:     descriptor.Mode,
			Label:    label,
			FocusKey: focusKey,
		})
	}
	current = strings.TrimSpace(current) // swobu:io-string source=boundary
	if shouldExposeCurrentAsOption(providerSpec, current) && !containsOptionValue(options, current) {
		options = append([]credentialChoiceOption{{Mode: profile.AuthMode(current), Label: current}}, options...)
	}
	items := make([]views.FilterablePickerItem, 0, len(options))
	for _, option := range options {
		choice := option
		key := strings.TrimSpace(string(choice.Mode)) // swobu:io-string source=boundary
		items = append(items, views.FilterablePickerItem{
			Key:      key,
			Label:    strings.TrimSpace(choice.Label),                             // swobu:io-string source=boundary
			Search:   string(choice.Mode) + " " + strings.TrimSpace(choice.Label), // swobu:io-string source=boundary
			Selected: string(choice.Mode) == current,
			OnChoose: func() []update.Action {
				if onChoose != nil {
					return onChoose(choice)
				}
				return nil
			},
		})
	}
	return items
}

func credentialOptionRows(
	current string,
	onChoose func(credentialChoiceOption) []update.Action,
	onCancel func() []update.Action,
	providerSpec string,
	_ bool,
) []retained.ViewSpec[state.Model] {
	items := credentialOptionItems(current, onChoose, providerSpec)
	rows := make([]retained.ViewSpec[state.Model], 0, len(items))
	for _, item := range items {
		choice := item
		rows = append(rows, toolkitviews.ListItemRow[state.Model](
			toolkitviews.InsetLabel(strings.TrimSpace(choice.Label), 3), // swobu:io-string source=boundary
			choice.Selected,
			true,
			true,
			choice.OnChoose,
			onCancel,
		))
	}
	return rows
}
