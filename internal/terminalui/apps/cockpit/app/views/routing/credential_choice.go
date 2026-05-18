// Provider credential choice row.
package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	stateModel "github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state/model"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
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
	parent := views.RowChoiceWithCancel(views.RowUseKeyFrom, current, func() []update.Action {
		nextOpen := !open
		setOpen(nextOpen)
		mode := state.InteractionModeManageList
		if nextOpen {
			mode = state.InteractionModePickOne
		}
		return []update.Action{state.SetInteractionMode{Mode: mode}}
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
		if current == "missing" {
			out = toolkitviews.NewAnchoredDisclosure(parent, views.DisclosureNoteRows("authentication needed - choose a credential ref to save")...)
		}
	} else {
		rows := credentialOptionRows(current, func(value string) []update.Action {
			setOpen(false)
			actions := applyProviderCredentialSelection(value, providerSpec, spec.ProviderConfig, spec.EndpointName, spec.CreateMode)
			actions = append(actions, state.SetInteractionMode{Mode: state.InteractionModeManageList})
			return actions
		}, func() []update.Action {
			setOpen(false)
			return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeManageList}}
		}, providerSpec, spec.CreateMode)
		out = toolkitviews.NewAnchoredDisclosure(parent, rows...)
	}
	return out
}

func applyProviderCredentialSelection(credentialRef string, providerSpec string, providerConfig *state.ProviderConfigSnapshot, endpointName string, createMode bool) []update.Action {
	credentialRef = strings.TrimSpace(credentialRef)                       // swobu:io-string source=boundary
	variant := providercatalog.AuthVariant(strings.ToLower(credentialRef)) // swobu:io-string source=boundary
	if providercatalog.IsInteractiveAuthVariant(variant) {
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
		credentialRef = encodeCredentialEnvRef(providercatalog.DefaultEnvKeyForSpec(providerSpec))
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

func credentialOptionRows(
	current string,
	onChoose func(string) []update.Action,
	onCancel func() []update.Action,
	providerSpec string,
	createMode bool,
) []retained.ViewSpec[state.Model] {
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
	descriptors := authModeDescriptorsForSpec(providerSpec)
	options := make([]option, 0, len(descriptors))
	for _, descriptor := range descriptors {
		label := descriptor.Label
		if strings.EqualFold(strings.TrimSpace(providerSpec), "bedrock") && descriptor.Variant == providercatalog.AuthVariantEnv { // swobu:io-string source=boundary
			label = "Bedrock API key"
		}
		options = append(options, option{
			Value: string(descriptor.Variant),
			Label: label,
		})
	}
	current = strings.TrimSpace(current) // swobu:io-string source=boundary
	if current != "" && current != "missing" && current != "signed in" && !containsOptionValue(options, current) {
		options = append([]option{{Value: current, Label: current}}, options...)
	}
	rows := make([]retained.ViewSpec[state.Model], 0, len(options))
	for _, option := range options {
		choice := option
		rows = append(rows, toolkitviews.ListItemRow[state.Model](
			toolkitviews.InsetLabel(strings.TrimSpace(choice.Label), 3), // swobu:io-string source=boundary
			choice.Value == current,
			true,
			true,
			func() []update.Action {
				if onChoose != nil {
					return onChoose(choice.Value)
				}
				return nil
			},
			onCancel,
		))
	}
	return rows
}
