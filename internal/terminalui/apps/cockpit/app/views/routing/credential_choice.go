// Provider credential choice row.
package routing

import (
	"strings"

	"github.com/metrofun/swobu/internal/domain/providercatalog"
	"github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/selectors"
	"github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/update"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/view"
	toolkitviews "github.com/metrofun/swobu/internal/terminalui/toolkit/views"
)

type providerCredentialChoiceRowSpec struct {
	ProviderConfig *state.ProviderConfigSnapshot
	EndpointName   string
	CreateMode     bool
}

func providerCredentialChoiceRow(spec providerCredentialChoiceRowSpec) view.ViewSpec[state.Model] {
	return view.Build[state.Model](func(ctx *view.Context[state.Model]) view.ViewSpec[state.Model] {
		return buildProviderCredentialChoiceRow(ctx, spec)
	})
}

func buildProviderCredentialChoiceRow(ctx *view.Context[state.Model], spec providerCredentialChoiceRowSpec) view.ViewSpec[state.Model] {
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
	open, setOpen := view.UseState(ctx, func() bool { return false })
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
	var out view.ViewSpec[state.Model]
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

func credentialOptionRows(current string, onChoose func(string) []update.Action, onCancel func() []update.Action) []view.ViewSpec[state.Model] {
	options := []string{"env", "keychain", "file"}
	current = strings.TrimSpace(current)
	if current != "" && current != "missing" && !containsString(options, current) {
		options = append([]string{current}, options...)
	}
	rows := make([]view.ViewSpec[state.Model], 0, len(options))
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
