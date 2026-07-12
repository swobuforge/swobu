package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

// FilterablePickerItemFocusKey returns the focus key for the item at index.
// If the item has an explicit Key, that stable identity wins; otherwise the
// picker uses a generated prefix/index key for unkeyed rows.
func FilterablePickerItemFocusKey(filtered []FilterablePickerItem, cfg FilterablePickerConfig, index int) string {
	if index < 0 {
		index = 0
	}
	if index >= len(filtered) {
		index = len(filtered) - 1
	}
	if index >= 0 && index < len(filtered) {
		if key := strings.TrimSpace(filtered[index].Key); key != "" { // swobu:io-string source=boundary
			return key
		}
	}
	return FilterablePickerFocusKey(cfg.KeyPrefix, index)
}

func focusActionsAfterQueryChange(items []FilterablePickerItem, cfg FilterablePickerConfig, query string) []update.Action {
	filtered := filterablePickerItems(items, query)
	if len(filtered) > 0 {
		return []update.Action{interaction.FocusKeyAction{Key: FilterablePickerItemFocusKey(filtered, cfg, 0)}}
	}
	return nil
}

// FilterablePickerFirstFocusKey returns the key for the first picker item when
// one exists. Empty lists intentionally stay unfocused so open actions do not
// invent a synthetic fallback identity.
func FilterablePickerFirstFocusKey(items []FilterablePickerItem, cfg FilterablePickerConfig) string {
	if len(items) == 0 {
		return ""
	}
	return FilterablePickerItemFocusKey(items, cfg, 0)
}

func defaultFilterablePickerOptionRow(showSelected bool) func(item FilterablePickerItem, onCancel func() []update.Action) retained.ViewSpec[state.Model] {
	return func(item FilterablePickerItem, onCancel func() []update.Action) retained.ViewSpec[state.Model] {
		return toolkitviews.ListItemRow[state.Model](
			toolkitviews.InsetLabel(strings.TrimSpace(item.Label), 3), // swobu:io-string source=boundary
			item.Selected,
			showSelected,
			true,
			item.OnChoose,
			onCancel,
		)
	}
}

func ChoicePickerOptionRow(showSelected bool) func(item FilterablePickerItem, onCancel func() []update.Action) retained.ViewSpec[state.Model] {
	if showSelected {
		return defaultFilterablePickerOptionRow(true)
	}
	return defaultFilterablePickerOptionRow(false)
}
