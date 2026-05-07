// Provider credential choice row.
package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
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
	model := ctx.Model()
	pc := selectedProvider(ctx.Model(), spec.ProviderConfig, spec.CreateMode)
	currentRef := ""
	if pc != nil {
		currentRef = strings.TrimSpace(pc.CredentialRef)
	}
	current := credentialSource(currentRef)
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
		} else if model.RoutingSaveError != "" {
			out = toolkitviews.NewAnchoredDisclosure(parent, views.DisclosureNoteRows(model.RoutingSaveError)...)
		}
	} else {
		providerSpec := ""
		if pc != nil {
			providerSpec = strings.TrimSpace(pc.ProviderSpec)
		}
		rows := credentialOptionRows(current, func(value string) []update.Action {
			setOpen(false)
			actions := applyProviderCredentialSelection(value, providerSpec, spec.ProviderConfig, spec.EndpointName, spec.CreateMode)
			actions = append(actions, state.SetInteractionMode{Mode: state.InteractionModeManageList})
			return actions
		}, func() []update.Action {
			setOpen(false)
			return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeManageList}}
		})
		out = toolkitviews.NewAnchoredDisclosure(parent, rows...)
	}
	return out
}

func applyProviderCredentialSelection(credentialRef string, providerSpec string, providerConfig *state.ProviderConfigSnapshot, endpointName string, createMode bool) []update.Action {
	credentialRef = strings.TrimSpace(credentialRef)
	if strings.EqualFold(credentialRef, "env") {
		credentialRef = encodeCredentialEnvRef(providercatalog.DefaultEnvKeyForSpec(providerSpec))
	}
	if strings.EqualFold(credentialRef, "file") {
		credentialRef = encodeCredentialFileRef("")
	}
	if createMode {
		return []update.Action{state.SetCreateDraftCredentialRef{CredentialRef: credentialRef}}
	}
	if providerConfig == nil || strings.TrimSpace(endpointName) == "" {
		return nil
	}
	next := *providerConfig
	next.CredentialRef = credentialRef
	return []update.Action{
		state.RoutingSaveStartedAction{},
		state.SaveProviderConfigRequested{
			EndpointName:   strings.TrimSpace(endpointName),
			ProviderConfig: next,
		},
	}
}

func credentialOptionRows(current string, onChoose func(string) []update.Action, onCancel func() []update.Action) []retained.ViewSpec[state.Model] {
	options := []string{"env", "keychain", "file"}
	current = strings.TrimSpace(current)
	if current != "" && current != "missing" && !containsString(options, current) {
		options = append([]string{current}, options...)
	}
	rows := make([]retained.ViewSpec[state.Model], 0, len(options))
	for _, option := range options {
		choice := option
		rows = append(rows, toolkitviews.NewChoiceOptionWithCancel[state.Model](choice, choice == current, func() []update.Action {
			if onChoose != nil {
				return onChoose(choice)
			}
			return nil
		}, onCancel))
	}
	return rows
}

func containsString(values []string, value string) bool {
	for _, item := range values {
		if strings.TrimSpace(item) == strings.TrimSpace(value) {
			return true
		}
	}
	return false
}
