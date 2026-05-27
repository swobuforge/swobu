package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func selectorsEmptyOr(value, fallback string) string {
	trimmed := trimRoutingInput(value)
	if trimmed != "" {
		return trimmed
	}
	return trimRoutingInput(fallback)
}

type bedrockProfilePickerRowSpec struct {
	Summary   string
	Current   string
	Profiles  []string
	CloseMode string
	FocusKey  string
	OnSave    func(string) []update.Action
}

func bedrockProfilePickerRow(ctx *retained.Context[state.Model], spec bedrockProfilePickerRowSpec) retained.ViewSpec[state.Model] {
	open, setOpen := retained.UseState(ctx, func() bool { return false })
	picker, setPicker := retained.UseState(ctx, func() views.FilterablePickerState { return views.DefaultFilterablePickerState() })
	parent := views.RowChoiceWithHooks("profile", spec.Summary, func() []update.Action {
		nextOpen := !open
		setOpen(nextOpen)
		if nextOpen {
			views.ResetFilterablePickerState(setPicker)
		}
		return bedrockProfilePickerToggleActions(nextOpen, spec.CloseMode)
	}, nil, views.FocusAffordance("choose", false))
	if !open {
		return parent
	}
	items := bedrockProfilePickerItems(spec.Profiles, spec.Current, func(value string) []update.Action {
		setOpen(false)
		actions := []update.Action{state.SetInteractionMode{Mode: spec.CloseMode}}
		if spec.OnSave != nil {
			actions = append(actions, spec.OnSave(value)...)
		}
		if spec.FocusKey != "" {
			actions = append(actions, interaction.FocusKeyAction{Key: spec.FocusKey})
		}
		return actions
	})
	if len(items) == 0 {
		items = append(items, views.FilterablePickerItem{Label: "no profiles found", Search: "", Selected: false, OnChoose: nil})
	}
	return views.RenderFilterablePickerDisclosure(ctx, parent, picker, setPicker, items, views.FilterablePickerConfig{
		KeyPrefix:      "bedrock-profile-option",
		BuildOptionRow: views.ChoicePickerOptionRow(false),
		WindowSize:     8,
		NonFilterable:  true,
		OnCancel: func() []update.Action {
			setOpen(false)
			actions := []update.Action{state.SetInteractionMode{Mode: spec.CloseMode}}
			if spec.FocusKey != "" {
				actions = append(actions, interaction.FocusKeyAction{Key: spec.FocusKey})
			}
			return actions
		},
	})
}

func bedrockProfilePickerItems(profiles []string, current string, onChoose func(string) []update.Action) []views.FilterablePickerItem {
	items := make([]views.FilterablePickerItem, 0, len(profiles)+1)
	current = trimRoutingInput(current)
	items = append(items, views.FilterablePickerItem{
		Label:    "auto",
		Search:   "auto aws chain default",
		Selected: current == "",
		OnChoose: func() []update.Action {
			if onChoose != nil {
				return onChoose("")
			}
			return nil
		},
	})
	for _, profile := range profiles {
		candidate := trimRoutingInput(profile)
		if candidate == "" {
			continue
		}
		value := candidate
		items = append(items, views.FilterablePickerItem{
			Label:    value,
			Search:   value,
			Selected: strings.EqualFold(value, current),
			OnChoose: func() []update.Action {
				if onChoose != nil {
					return onChoose(value)
				}
				return nil
			},
		})
	}
	return items
}

func bedrockProfilePickerToggleActions(open bool, closeMode string) []update.Action {
	if open {
		return []update.Action{
			interaction.FocusKeyAction{Key: views.FilterablePickerFocusKey("bedrock-profile-option", 0)},
			state.SetInteractionMode{Mode: state.InteractionModePickOne},
		}
	}
	return []update.Action{state.SetInteractionMode{Mode: closeMode}}
}

type bedrockRegionPickerRowSpec struct {
	Label      string
	Summary    string
	Current    string
	CloseMode  string
	FocusKey   string
	EditorHint string
	OnSave     func(string) []update.Action
}

func bedrockRegionPickerRow(ctx *retained.Context[state.Model], spec bedrockRegionPickerRowSpec) retained.ViewSpec[state.Model] {
	regions := bedrockRegions()
	if len(regions) == 0 {
		return backendURLEditorRow(ctx, spec.Label, spec.Summary, spec.Current, spec.EditorHint, spec.OnSave)
	}
	open, setOpen := retained.UseState(ctx, func() bool { return false })
	picker, setPicker := retained.UseState(ctx, func() views.FilterablePickerState { return views.DefaultFilterablePickerState() })
	parent := views.RowChoiceWithCancel(spec.Label, spec.Summary, func() []update.Action {
		nextOpen := !open
		setOpen(nextOpen)
		if nextOpen {
			views.ResetFilterablePickerState(setPicker)
			return []update.Action{
				state.SetInteractionMode{Mode: state.InteractionModePickOne},
				interaction.FocusKeyAction{Key: views.FilterablePickerFocusKey("bedrock-region-option", 0)},
			}
		}
		return []update.Action{
			state.SetInteractionMode{Mode: spec.CloseMode},
			interaction.FocusKeyAction{Key: spec.FocusKey},
		}
	}, func() []update.Action {
		if !open {
			return nil
		}
		setOpen(false)
		return []update.Action{
			state.SetInteractionMode{Mode: spec.CloseMode},
			interaction.FocusKeyAction{Key: spec.FocusKey},
		}
	})
	if !open {
		return parent
	}
	items := make([]views.FilterablePickerItem, 0, len(regions))
	for _, region := range regions {
		choice := trimRoutingInput(region)
		if choice == "" {
			continue
		}
		items = append(items, views.FilterablePickerItem{
			Label:    choice,
			Search:   choice,
			Selected: strings.EqualFold(choice, trimRoutingInput(spec.Current)),
			OnChoose: func() []update.Action {
				setOpen(false)
				actions := spec.OnSave(choice)
				return append(actions,
					state.SetInteractionMode{Mode: spec.CloseMode},
					interaction.FocusKeyAction{Key: spec.FocusKey},
				)
			},
		})
	}
	if len(items) == 0 {
		return backendURLEditorRow(ctx, spec.Label, spec.Summary, spec.Current, spec.EditorHint, spec.OnSave)
	}
	return views.RenderFilterablePickerDisclosure(ctx, parent, picker, setPicker, items, views.FilterablePickerConfig{
		KeyPrefix:      "bedrock-region-option",
		BuildOptionRow: views.ChoicePickerOptionRow(false),
		WindowSize:     8,
		FindLabel:      "find",
		ShowSelected:   true,
		OnNoMatchFocus: func() []update.Action {
			return []update.Action{interaction.FocusKeyAction{Key: spec.FocusKey}}
		},
		OnCancel: func() []update.Action {
			setOpen(false)
			return []update.Action{
				state.SetInteractionMode{Mode: spec.CloseMode},
				interaction.FocusKeyAction{Key: spec.FocusKey},
			}
		},
	})
}
