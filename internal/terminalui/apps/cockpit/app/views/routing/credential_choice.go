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
		currentRef = strings.TrimSpace(pc.CredentialRef)  // trimlowerlint:allow boundary canonicalization
		providerSpec = strings.TrimSpace(pc.ProviderSpec) // trimlowerlint:allow boundary canonicalization
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
	credentialRef = strings.TrimSpace(credentialRef)                       // trimlowerlint:allow boundary canonicalization
	variant := providercatalog.AuthVariant(strings.ToLower(credentialRef)) // trimlowerlint:allow boundary canonicalization
	if providercatalog.IsInteractiveAuthVariant(variant) {
		if createMode {
			return []update.Action{state.SetCreateDraftCredentialRef{CredentialRef: credentialRef}}
		}
		if providerConfig == nil || strings.TrimSpace(endpointName) == "" { // trimlowerlint:allow boundary canonicalization
			return nil
		}
		return []update.Action{state.StartProviderAuthSessionRequested{
			EndpointName:   strings.TrimSpace(endpointName), // trimlowerlint:allow boundary canonicalization
			ProviderConfig: *providerConfig,
			OwnerKey:       stateModel.EndpointProviderAuthOwnerKey(strings.TrimSpace(endpointName), strings.TrimSpace(providerConfig.Ref)).String(), // trimlowerlint:allow boundary canonicalization
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
	if providerConfig == nil || strings.TrimSpace(endpointName) == "" { // trimlowerlint:allow boundary canonicalization
		return nil
	}
	next := *providerConfig
	next.CredentialRef = credentialRef
	return routingSaveProviderConfigActions(strings.TrimSpace(endpointName), next, "provider/auth") // trimlowerlint:allow boundary canonicalization
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
			if strings.TrimSpace(item.Value) == strings.TrimSpace(value) { // trimlowerlint:allow boundary canonicalization
				return true
			}
		}
		return false
	}
	variants := providercatalog.SupportedAuthVariantsForSpec(strings.TrimSpace(providerSpec)) // trimlowerlint:allow boundary canonicalization
	options := make([]option, 0, len(variants))
	for _, v := range variants {
		options = append(options, option{
			Value: string(v),
			Label: authVariantDisplayLabel(v), // trimlowerlint:allow boundary canonicalization
		})
	}
	current = strings.TrimSpace(current) // trimlowerlint:allow boundary canonicalization
	if current != "" && current != "missing" && current != "signed in" && !containsOptionValue(options, current) {
		options = append([]option{{Value: current, Label: current}}, options...)
	}
	rows := make([]retained.ViewSpec[state.Model], 0, len(options))
	for _, option := range options {
		choice := option
		rows = append(rows, toolkitviews.ListItemRow[state.Model](
			toolkitviews.InsetLabel(strings.TrimSpace(choice.Label), 3), // trimlowerlint:allow boundary canonicalization
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
