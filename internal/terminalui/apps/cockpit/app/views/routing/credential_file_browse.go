package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// providerCredentialFileBrowseRowSpec owns credential-file browse behavior when file-backed credentials are selected.
type providerCredentialFileBrowseRowSpec struct {
	ProviderConfig *state.ProviderConfigSnapshot
	EndpointName   string
	CreateMode     bool
}

func providerCredentialFileBrowseRow(spec providerCredentialFileBrowseRowSpec) retained.ViewSpec[state.Model] {
	return retained.Build[state.Model](func(ctx *retained.Context[state.Model]) retained.ViewSpec[state.Model] {
		return buildProviderCredentialFileBrowseRow(ctx, spec)
	})
}

func buildProviderCredentialFileBrowseRow(ctx *retained.Context[state.Model], spec providerCredentialFileBrowseRowSpec) retained.ViewSpec[state.Model] {
	pc := selectedProvider(ctx.Model(), spec.ProviderConfig, spec.CreateMode)
	if pc == nil || !strings.EqualFold(credentialSource(pc.CredentialRef), "file") {
		return nil
	}
	closeMode := state.InteractionModeManageList
	if spec.CreateMode {
		closeMode = state.InteractionModeNAV
	}
	currentPath := credentialFilePath(pc.CredentialRef)
	open, setOpen := retained.UseState(ctx, func() bool { return false })
	browse, setBrowse := retained.UseState(ctx, func() credentialFileBrowseState { return initialCredentialFileBrowseState(currentPath) })
	picker, setPicker := retained.UseState(ctx, func() views.FilterablePickerState { return views.DefaultFilterablePickerState() })
	parent := credentialFileRow(currentPath, func() []update.Action {
		nextOpen := !open
		setOpen(nextOpen)
		actions := make([]update.Action, 0, 2)
		if nextOpen {
			nextBrowse := initialCredentialFileBrowseState(currentPath)
			setBrowse(nextBrowse)
			views.ResetFilterablePickerState(setPicker)
			actions = append(actions, interaction.FocusKeyAction{Key: views.FilterablePickerFocusKey("credential-file-option", 0)})
		}
		if nextOpen {
			actions = append(actions, state.SetInteractionMode{Mode: state.InteractionModePickOne})
		} else {
			actions = append(actions, state.SetInteractionMode{Mode: closeMode})
		}
		return actions
	}, func() []update.Action {
		if open {
			setOpen(false)
			return []update.Action{
				state.SetInteractionMode{Mode: closeMode},
				interaction.FocusKeyAction{Key: "credential-file"},
			}
		}
		return nil
	})
	out := parent
	if !open {
	} else {
		items, err := credentialFilePickerItems(browse, setBrowse, func() []update.Action {
			views.ResetFilterablePickerState(setPicker)
			return []update.Action{
				interaction.FocusKeyAction{Key: views.FilterablePickerFocusKey("credential-file-option", 0)},
			}
		}, currentPath, func(path string) []update.Action {
			setOpen(false)
			actions := applyProviderCredentialFileSelection(path, spec.ProviderConfig, spec.EndpointName, spec.CreateMode)
			actions = append(actions,
				state.SetInteractionMode{Mode: closeMode},
				interaction.FocusKeyAction{Key: "credential-file"},
			)
			return actions
		})
		if err != nil {
			out = toolkitviews.NewAnchoredDisclosure(parent, views.DisclosureNoteRows(err.Error())...)
		} else {
			out = views.RenderFilterablePickerDisclosure(ctx, parent, picker, setPicker, items, views.FilterablePickerConfig{
				KeyPrefix:      "credential-file-option",
				BuildOptionRow: views.ChoicePickerOptionRow(false),
				WindowSize:     6,
				FindLabel:      "find",
				NoMatchesLabel: "no files",
				HeaderRows: []retained.ViewSpec[state.Model]{
					views.RowStatic("path", credentialFileBrowserPath(browse.Dir)),
				},
				OnNoMatchFocus: func() []update.Action { return []update.Action{interaction.FocusKeyAction{Key: "credential-file"}} },
				OnCancel: func() []update.Action {
					setOpen(false)
					return []update.Action{
						state.SetInteractionMode{Mode: closeMode},
						interaction.FocusKeyAction{Key: "credential-file"},
					}
				},
			})
		}
	}
	return out
}

func applyProviderCredentialFileSelection(path string, providerConfig *state.ProviderConfigSnapshot, endpointName string, createMode bool) []update.Action {
	ref := encodeCredentialFileRef(path)
	if createMode {
		return []update.Action{state.SetCreateDraftCredentialRef{CredentialRef: ref}}
	}
	if providerConfig == nil || strings.TrimSpace(endpointName) == "" {
		return nil
	}
	next := *providerConfig
	next.CredentialRef = ref
	return []update.Action{
		state.RoutingSaveStartedAction{},
		state.SaveProviderConfigRequested{
			EndpointName:   strings.TrimSpace(endpointName),
			ProviderConfig: next,
		},
	}
}
