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
	current := credentialSource(currentRef)
	if isResolvedInteractiveCredential(providerSpec, currentRef) {
		current = "signed in"
	}
	if current == "" {
		current = selectors.CredentialSummaryFromProviderConfig(pc)
	}
	open, setOpen := retained.UseState(ctx, func() bool { return false })
	picker, setPicker := retained.UseState(ctx, func() views.FilterablePickerState { return views.DefaultFilterablePickerState() })
	items := credentialOptionItems(current, func(value string) []update.Action {
		setOpen(false)
		actions := applyProviderCredentialSelection(value, providerSpec, spec.ProviderConfig, spec.EndpointName, spec.CreateMode)
		actions = append(actions, state.SetInteractionMode{Mode: state.InteractionModeManageList})
		if focusKey := credentialPostSelectionFocusKey(value); focusKey != "" {
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

func credentialPostSelectionFocusKey(raw string) string {
	lowered := strings.ToLower(strings.TrimSpace(raw)) // swobu:io-string source=boundary
	if lowered == "file" {
		return "credential-file"
	}
	if lowered == "env" || lowered == "env var" {
		return "env"
	}
	return ""
}

func applyProviderCredentialSelection(credentialRef string, providerSpec string, providerConfig *state.ProviderConfigSnapshot, endpointName string, createMode bool) []update.Action {
	credentialRef = strings.TrimSpace(credentialRef)               // swobu:io-string source=boundary
	variant := profile.AuthVariant(strings.ToLower(credentialRef)) // swobu:io-string source=boundary
	if profile.IsInteractiveAuthVariant(variant) {
		if createMode {
			return []update.Action{state.SetCreateDraftCredentialRef{CredentialRef: credentialRef}}
		}
		if providerConfig == nil || strings.TrimSpace(endpointName) == "" { // swobu:io-string source=boundary
			return nil
		}
		return []update.Action{state.StartProviderAuthSessionRequested{
			EndpointName:   strings.TrimSpace(endpointName), // swobu:io-string source=boundary
			ProviderConfig: *providerConfig,
			OwnerKey:       stateModel.EndpointProviderAuthOwnerKey(strings.TrimSpace(endpointName), strings.TrimSpace(providerConfig.Ref)).String(), // swobu:io-string source=boundary
			AuthScope:      stateModel.AuthScopeEndpointProvider,
		}}
	}
	if strings.EqualFold(credentialRef, "env") {
		credentialRef = encodeCredentialEnvRef(profile.DefaultEnvKeyForSpec(providerSpec))
	}
	if strings.EqualFold(credentialRef, "file") {
		credentialRef = encodeCredentialFileRef("")
	}
	if createMode {
		return []update.Action{state.SetCreateDraftCredentialRef{CredentialRef: credentialRef}}
	}
	if providerConfig == nil || strings.TrimSpace(endpointName) == "" { // swobu:io-string source=boundary
		return nil
	}
	next := *providerConfig
	next.CredentialRef = credentialRef
	return routingSaveProviderConfigActions(strings.TrimSpace(endpointName), next, "provider/auth") // swobu:io-string source=boundary
}

func credentialOptionItems(
	current string,
	onChoose func(string) []update.Action,
	providerSpec string,
) []views.FilterablePickerItem {
	type option struct {
		Value string
		Label string
	}
	containsOptionValue := func(values []option, value string) bool {
		for _, item := range values {
			if strings.TrimSpace(item.Value) == strings.TrimSpace(value) { // swobu:io-string source=boundary
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
	options := make([]option, 0, len(descriptors))
	for _, descriptor := range descriptors {
		if strings.EqualFold(strings.TrimSpace(providerSpec), "bedrock") && descriptor.Variant == profile.AuthVariantAWSEnvSession { // swobu:io-string source=boundary
			// Bedrock exposes one canonical AWS chain affordance in UI.
			continue
		}
		label := descriptor.Label
		if strings.EqualFold(strings.TrimSpace(providerSpec), "bedrock") && descriptor.Variant == profile.AuthVariantEnv { // swobu:io-string source=boundary
			label = "Bedrock API key"
		}
		options = append(options, option{
			Value: string(descriptor.Variant),
			Label: label,
		})
	}
	current = strings.TrimSpace(current) // swobu:io-string source=boundary
	if shouldExposeCurrentAsOption(providerSpec, current) && !containsOptionValue(options, current) {
		options = append([]option{{Value: current, Label: current}}, options...)
	}
	items := make([]views.FilterablePickerItem, 0, len(options))
	for _, option := range options {
		choice := option
		items = append(items, views.FilterablePickerItem{
			Key:      strings.TrimSpace(choice.Value),
			Label:    strings.TrimSpace(choice.Label),                      // swobu:io-string source=boundary
			Search:   choice.Value + " " + strings.TrimSpace(choice.Label), // swobu:io-string source=boundary
			Selected: choice.Value == current,
			OnChoose: func() []update.Action {
				if onChoose != nil {
					return onChoose(choice.Value)
				}
				return nil
			},
		})
	}
	return items
}

func credentialOptionRows(
	current string,
	onChoose func(string) []update.Action,
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
