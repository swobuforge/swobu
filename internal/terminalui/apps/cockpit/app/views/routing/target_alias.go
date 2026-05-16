package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

type providerTargetAliasRowSpec struct {
	ProviderConfig *state.ProviderConfigSnapshot
	EndpointName   string
	CreateMode     bool
}

func providerTargetAliasRow(spec providerTargetAliasRowSpec) retained.ViewSpec[state.Model] {
	return retained.Build[state.Model](func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		return buildProviderTargetAliasRow(ctx, spec)
	})
}

func buildProviderTargetAliasRow(ctx *retained.Context[state.Model], spec providerTargetAliasRowSpec) retained.ViewSpec[state.Model] {
	pc := selectedProvider(ctx.Model(), spec.ProviderConfig, spec.CreateMode)
	if pc == nil {
		return nil
	}
	currentValue := strings.TrimSpace(pc.TargetAlias) // trimlowerlint:allow boundary canonicalization
	summary := "not set"
	if currentValue != "" {
		summary = currentValue
	}
	row := aliasInlineEditorRow(ctx, summary, currentValue, "fast", func(value string) []update.Action {
		return applyProviderTargetAlias(value, spec.ProviderConfig, spec.EndpointName, spec.CreateMode)
	})
	if message := views.ScopedError(ctx.Model(), "routing", "provider/alias"); message != "" {
		return toolkitviews.NewAnchoredDisclosure(row, views.DisclosureNoteRows(message)...)
	}
	return row
}

func aliasInlineEditorRow(
	ctx *retained.Context[state.Model],
	summary string,
	currentValue string,
	emptyStateLabel string,
	save func(string) []update.Action,
) retained.ViewSpec[state.Model] {
	open, setOpen := retained.UseState(ctx, func() bool { return false })
	draft, setDraft := retained.UseState(ctx, func() string { return currentValue })
	if open {
		return retained.Named[state.Model]("alias-inline-editor", views.InlineEditor(
			views.RowTargetAlias,
			draft,
			emptyStateLabel,
			func(value string) []update.Action {
				setDraft(value)
				return nil
			},
			func(value string) []update.Action {
				setOpen(false)
				actions := save(strings.TrimSpace(value)) // trimlowerlint:allow boundary canonicalization
				return append([]update.Action{state.SetInteractionMode{Mode: state.InteractionModeManageList}}, actions...)
			},
			func() []update.Action {
				setOpen(false)
				setDraft(currentValue)
				return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeManageList}}
			},
		))
	}
	return views.RowEditWithCancel(
		views.RowTargetAlias,
		summary,
		func() []update.Action {
			setDraft(currentValue)
			setOpen(true)
			return []update.Action{state.SetInteractionMode{Mode: state.InteractionModeEditText}}
		},
		nil,
	)
}

func applyProviderTargetAlias(targetAlias string, providerConfig *state.ProviderConfigSnapshot, endpointName string, createMode bool) []update.Action {
	targetAlias = strings.ToLower(strings.TrimSpace(targetAlias)) // trimlowerlint:allow boundary canonicalization
	if createMode {
		return []update.Action{state.SetCreateDraftTargetAlias{TargetAlias: targetAlias}}
	}
	if providerConfig == nil || strings.TrimSpace(endpointName) == "" { // trimlowerlint:allow boundary canonicalization
		return nil
	}
	next := *providerConfig
	next.TargetAlias = targetAlias
	return routingSaveProviderConfigActions(strings.TrimSpace(endpointName), next, "provider/alias") // trimlowerlint:allow boundary canonicalization
}
