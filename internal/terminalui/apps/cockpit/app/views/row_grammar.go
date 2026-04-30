package views

import (
	"github.com/metrofun/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/update"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/view"
	toolkitviews "github.com/metrofun/swobu/internal/terminalui/toolkit/views"
)

func cockpitRowPolicy() toolkitviews.RowLayoutPolicy {
	p := toolkitviews.DefaultRowLayoutPolicy()
	p.ActionStartCol = 65
	return p
}

func RowKV(label, value, action string, onActivate func() []update.Action) view.ViewSpec[state.Model] {
	return toolkitviews.NewKeyValueActionRowWithPolicy[state.Model](label, value, action, cockpitRowPolicy(), onActivate)
}

func RowKVWithCancel(label, value, action string, onActivate func() []update.Action, onCancel func() []update.Action) view.ViewSpec[state.Model] {
	return toolkitviews.NewKeyValueActionRowWithCancelAndPolicy[state.Model](label, value, action, cockpitRowPolicy(), onActivate, onCancel)
}

func RowKVWithHooks(label, value, action string, onActivate func() []update.Action, onCancel func() []update.Action, onFocus func() []update.Action) view.ViewSpec[state.Model] {
	return toolkitviews.NewKeyValueActionRowWithHooksAndPolicy[state.Model](label, value, action, cockpitRowPolicy(), onActivate, onCancel, onFocus)
}

func RowStatic(label, value string) view.ViewSpec[state.Model] {
	return RowKV(label, value, "", nil)
}

func RowAction(label, value, verb string, onActivate func() []update.Action) view.ViewSpec[state.Model] {
	return RowKV(label, value, verb+" ↵", onActivate)
}

func RowActionWithCancel(label, value, verb string, onActivate func() []update.Action, onCancel func() []update.Action) view.ViewSpec[state.Model] {
	return RowKVWithCancel(label, value, verb+" ↵", onActivate, onCancel)
}

func RowActionWithHooks(label, value, verb string, onActivate func() []update.Action, onCancel func() []update.Action, onFocus func() []update.Action) view.ViewSpec[state.Model] {
	return RowKVWithHooks(label, value, verb+" ↵", onActivate, onCancel, onFocus)
}

func RowChoiceWithHooks(label, value string, onActivate func() []update.Action, onCancel func() []update.Action, onFocus func() []update.Action) view.ViewSpec[state.Model] {
	return RowKVWithHooks(label, value, "choose ↵", onActivate, onCancel, onFocus)
}

func RowChoiceWithCancel(label, value string, onActivate func() []update.Action, onCancel func() []update.Action) view.ViewSpec[state.Model] {
	return RowKVWithCancel(label, value, "choose ↵", onActivate, onCancel)
}

func RowManageWithHooks(label, value string, onActivate func() []update.Action, onCancel func() []update.Action, onFocus func() []update.Action) view.ViewSpec[state.Model] {
	return RowKVWithHooks(label, value, "manage ↵", onActivate, onCancel, onFocus)
}

func RowEditWithHooks(label, value string, onActivate func() []update.Action, onCancel func() []update.Action, onFocus func() []update.Action) view.ViewSpec[state.Model] {
	return RowKVWithHooks(label, value, "edit ↵", onActivate, onCancel, onFocus)
}

func RowEditWithCancel(label, value string, onActivate func() []update.Action, onCancel func() []update.Action) view.ViewSpec[state.Model] {
	return RowKVWithCancel(label, value, "edit ↵", onActivate, onCancel)
}

func InlineEditor(label, value, emptyValue string, onChange func(string) []update.Action, onCommit func(string) []update.Action, onCancel func() []update.Action) view.ViewSpec[state.Model] {
	return toolkitviews.NewInlineEditor[state.Model](label, value, emptyValue, cockpitRowPolicy(), onChange, onCommit, onCancel)
}
