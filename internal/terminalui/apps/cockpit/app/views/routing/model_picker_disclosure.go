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
	Key      string
	Label    string
	Selected bool
	OnChoose func() []update.Action
}

type modelPickerRenderSpec struct {
	Parent          retained.ViewSpec[state.Model]
	Picker          views.FilterablePickerState
	SetPicker       func(views.FilterablePickerState)
	Options         []modelPickerOption
	HeaderRows      []retained.ViewSpec[state.Model]
	KeyPrefix       string
	FocusKey        string
	CloseDisclosure func() []update.Action
}

func renderModelPickerDisclosure(ctx *retained.Context[state.Model], spec modelPickerRenderSpec) retained.ViewSpec[state.Model] {
	items := buildModelPickerItems(spec.Options)
	return views.RenderFilterablePickerDisclosure(ctx, spec.Parent, spec.Picker, spec.SetPicker, items, views.FilterablePickerConfig{
		KeyPrefix:      spec.KeyPrefix,
		BuildOptionRow: views.ChoicePickerOptionRow(false),
		WindowSize:     6,
		FindLabel:      "find",
		HeaderRows:     append([]retained.ViewSpec[state.Model](nil), spec.HeaderRows...),
		OnNoMatchFocus: func() []update.Action { return []update.Action{interaction.FocusKeyAction{Key: spec.FocusKey}} },
		OnCancel:       spec.CloseDisclosure,
	})
}

func buildModelPickerItems(options []modelPickerOption) []views.FilterablePickerItem {
	items := make([]views.FilterablePickerItem, 0, len(options))
	for _, opt := range options {
		option := opt
		key := strings.TrimSpace(option.Key)
		if key == "" {
			key = strings.TrimSpace(option.Label)
		}
		items = append(items, views.FilterablePickerItem{
			Key:      key,
			Label:    option.Label,
			Selected: option.Selected,
			OnChoose: option.OnChoose,
		})
	}
	return items
}

func modelPickerFirstFocusKey(options []modelPickerOption, keyPrefix string) string {
	return views.FilterablePickerFirstFocusKey(buildModelPickerItems(options), views.FilterablePickerConfig{KeyPrefix: keyPrefix})
}
