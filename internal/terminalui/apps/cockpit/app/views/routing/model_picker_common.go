package routing

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/views"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

type modelPickerOption struct {
	Label    string
	Selected bool
	OnChoose func() []update.Action
}

type modelPickerRenderSpec struct {
	Parent          retained.ViewSpec[state.Model]
	Picker          views.FilterablePickerState
	SetPicker       func(views.FilterablePickerState)
	Options         []modelPickerOption
	OnChooseRawID   func(string) []update.Action
	KeyPrefix       string
	FocusKey        string
	CloseDisclosure func() []update.Action
}

func renderModelPickerDisclosure(ctx *retained.Context[state.Model], spec modelPickerRenderSpec) retained.ViewSpec[state.Model] {
	items := buildModelPickerItems(spec.Options, spec.Picker.Query, spec.OnChooseRawID)
	return views.RenderFilterablePickerDisclosure(ctx, spec.Parent, spec.Picker, spec.SetPicker, items, views.FilterablePickerConfig{
		KeyPrefix:      spec.KeyPrefix,
		BuildOptionRow: views.ChoicePickerOptionRow(false),
		WindowSize:     6,
		FindLabel:      "find",
		OnNoMatchFocus: func() []update.Action { return []update.Action{interaction.FocusKeyAction{Key: spec.FocusKey}} },
		OnCancel:       spec.CloseDisclosure,
	})
}

func buildModelPickerItems(options []modelPickerOption, query string, onChooseRawID func(string) []update.Action) []views.FilterablePickerItem {
	items := make([]views.FilterablePickerItem, 0, len(options)+1)
	for _, opt := range options {
		option := opt
		items = append(items, views.FilterablePickerItem{
			Label:    option.Label,
			Selected: option.Selected,
			OnChoose: option.OnChoose,
		})
	}
	rawID := strings.TrimSpace(query)
	if rawID == "" || onChooseRawID == nil {
		return items
	}
	for _, opt := range options {
		if strings.EqualFold(strings.TrimSpace(opt.Label), rawID) {
			return items
		}
	}
	items = append([]views.FilterablePickerItem{{
		Label:    "use: " + rawID,
		Selected: false,
		OnChoose: func() []update.Action { return onChooseRawID(rawID) },
	}}, items...)
	return items
}
