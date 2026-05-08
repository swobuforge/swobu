package routing

import (
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
	KeyPrefix       string
	FocusKey        string
	CloseDisclosure func() []update.Action
}

func renderModelPickerDisclosure(ctx *retained.Context[state.Model], spec modelPickerRenderSpec) retained.ViewSpec[state.Model] {
	items := make([]views.FilterablePickerItem, 0, len(spec.Options))
	for _, opt := range spec.Options {
		option := opt
		items = append(items, views.FilterablePickerItem{
			Label:    option.Label,
			Selected: option.Selected,
			OnChoose: option.OnChoose,
		})
	}
	return views.RenderFilterablePickerDisclosure(ctx, spec.Parent, spec.Picker, spec.SetPicker, items, views.FilterablePickerConfig{
		KeyPrefix:      spec.KeyPrefix,
		BuildOptionRow: views.ChoicePickerOptionRow(false),
		WindowSize:     6,
		FindLabel:      "find",
		OnNoMatchFocus: func() []update.Action { return []update.Action{interaction.FocusKeyAction{Key: spec.FocusKey}} },
		OnCancel:       spec.CloseDisclosure,
	})
}
