package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func RenderKeyValueTextLine(width int, key string, value string, keyWidth int) string {
	if width <= 0 {
		return ""
	}
	line := FormatKeyValueTextLine(key, value, keyWidth)
	return PadRight(TrimToWidth(line, width), width)
}

func NewChoiceOption[M any](label string, selected bool, onChoose func() []update.Action) retained.ViewSpec[M] {
	return NewChoiceOptionWithCancel[M](label, selected, onChoose, nil)
}

func NewChoiceOptionWithCancel[M any](label string, selected bool, onChoose func() []update.Action, onCancel func() []update.Action) retained.ViewSpec[M] {
	return ListItemRow[M](
		InsetLabel(strings.TrimSpace(label), 3), // swobu:io-string source=boundary
		selected,
		true,
		true,
		onChoose,
		onCancel,
	)
}

func NewKeyValueActionRowWithCancel[M any](label, value, action string, onActivate func() []update.Action, onCancel func() []update.Action) retained.ViewSpec[M] {
	return newRowViewSpec(rowViewSpec[M]{label: label, value: value, action: action, policy: DefaultLineLayoutPolicy(), onActivate: onActivate, onCancel: onCancel})
}

func NewKeyValueActionRowWithHooks[M any](label, value, action string, onActivate func() []update.Action, onCancel func() []update.Action, onFocus func() []update.Action) retained.ViewSpec[M] {
	return newRowViewSpec(rowViewSpec[M]{label: label, value: value, action: action, policy: DefaultLineLayoutPolicy(), onActivate: onActivate, onCancel: onCancel, onFocus: onFocus})
}

func NewStaticValueRow[M any](label, value string) retained.ViewSpec[M] {
	return NewKeyValueActionRow[M](label, value, "", nil)
}

func NewActionRow[M any](label, value, verb string, onActivate func() []update.Action) retained.ViewSpec[M] {
	return NewKeyValueActionRow[M](label, value, verb+" ↵", onActivate)
}

func NewActionRowWithCancel[M any](label, value, verb string, onActivate func() []update.Action, onCancel func() []update.Action) retained.ViewSpec[M] {
	return NewKeyValueActionRowWithCancel[M](label, value, verb+" ↵", onActivate, onCancel)
}

func NewActionRowWithHooks[M any](label, value, verb string, onActivate func() []update.Action, onCancel func() []update.Action, onFocus func() []update.Action) retained.ViewSpec[M] {
	return NewKeyValueActionRowWithHooks[M](label, value, verb+" ↵", onActivate, onCancel, onFocus)
}

func NewChoiceRow[M any](label, value string, onActivate func() []update.Action) retained.ViewSpec[M] {
	return NewKeyValueActionRow[M](label, value, "choose ↵", onActivate)
}

func NewChoiceRowWithCancel[M any](label, value string, onActivate func() []update.Action, onCancel func() []update.Action) retained.ViewSpec[M] {
	return NewKeyValueActionRowWithCancel[M](label, value, "choose ↵", onActivate, onCancel)
}

func NewChoiceRowWithHooks[M any](label, value string, onActivate func() []update.Action, onCancel func() []update.Action, onFocus func() []update.Action) retained.ViewSpec[M] {
	return NewKeyValueActionRowWithHooks[M](label, value, "choose ↵", onActivate, onCancel, onFocus)
}

func NewManageRow[M any](label, value string, onActivate func() []update.Action) retained.ViewSpec[M] {
	return NewKeyValueActionRow[M](label, value, "manage ↵", onActivate)
}

func NewManageRowWithCancel[M any](label, value string, onActivate func() []update.Action, onCancel func() []update.Action) retained.ViewSpec[M] {
	return NewKeyValueActionRowWithCancel[M](label, value, "manage ↵", onActivate, onCancel)
}

func NewManageRowWithHooks[M any](label, value string, onActivate func() []update.Action, onCancel func() []update.Action, onFocus func() []update.Action) retained.ViewSpec[M] {
	return NewKeyValueActionRowWithHooks[M](label, value, "manage ↵", onActivate, onCancel, onFocus)
}

func NewEditRow[M any](label, value string, onActivate func() []update.Action) retained.ViewSpec[M] {
	return NewKeyValueActionRow[M](label, value, "edit ↵", onActivate)
}

func NewEditRowWithCancel[M any](label, value string, onActivate func() []update.Action, onCancel func() []update.Action) retained.ViewSpec[M] {
	return NewKeyValueActionRowWithCancel[M](label, value, "edit ↵", onActivate, onCancel)
}

func NewEditRowWithHooks[M any](label, value string, onActivate func() []update.Action, onCancel func() []update.Action, onFocus func() []update.Action) retained.ViewSpec[M] {
	return NewKeyValueActionRowWithHooks[M](label, value, "edit ↵", onActivate, onCancel, onFocus)
}

func NewToggleRow[M any](label string, enabled bool, onActivate func() []update.Action) retained.ViewSpec[M] {
	v := "off"
	if enabled {
		v = "on"
	}
	return NewKeyValueActionRow[M](label, v, "toggle space", onActivate)
}

func NewEvidenceRow[M any](requestID, operation, target, timing, result string, onActivate func() []update.Action) retained.ViewSpec[M] {
	parts := []string{strings.TrimSpace(target), strings.TrimSpace(timing), strings.TrimSpace(result), strings.TrimSpace(operation)} // swobu:io-string source=boundary
	filtered := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			filtered = append(filtered, p)
		}
	}
	action := ""
	if onActivate != nil {
		action = "view ↵"
	}
	return NewKeyValueActionRow[M](strings.TrimSpace(requestID), strings.Join(filtered, "   "), action, onActivate) // swobu:io-string source=boundary
}
