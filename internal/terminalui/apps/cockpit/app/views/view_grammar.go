package views

import (
	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func cockpitRowPolicy() toolkitviews.LineLayoutPolicy {
	policy := toolkitviews.DefaultLineLayoutPolicy()
	if policy.MinLabelWidth < 4 {
		policy.MinLabelWidth = 4
	}
	return policy
}

func RowKV(label, value, action string, onActivate func() []update.Action) retained.ViewSpec[state.Model] {
	return toolkitviews.NewKeyValueActionRowWithPolicy[state.Model](label, value, action, cockpitRowPolicy(), onActivate)
}

func RowKVWithCancel(label, value, action string, onActivate func() []update.Action, onCancel func() []update.Action) retained.ViewSpec[state.Model] {
	return toolkitviews.NewKeyValueActionRowWithCancelAndPolicy[state.Model](label, value, action, cockpitRowPolicy(), onActivate, onCancel)
}

func RowKVWithHooks(label, value, action string, onActivate func() []update.Action, onCancel func() []update.Action, onFocus func() []update.Action) retained.ViewSpec[state.Model] {
	return toolkitviews.NewKeyValueActionRowWithHooksAndPolicy[state.Model](label, value, action, cockpitRowPolicy(), onActivate, onCancel, onFocus)
}

func RowStatic(label, value string) retained.ViewSpec[state.Model] {
	return RowKV(label, value, "", nil)
}

func RowAction(label, value, verb string, onActivate func() []update.Action) retained.ViewSpec[state.Model] {
	return RowKV(label, value, verb+" ↵", onActivate)
}

func RowActionWideValue(label, value, verb string, onActivate func() []update.Action) retained.ViewSpec[state.Model] {
	policy := cockpitRowPolicy()
	policy.StandardLabelWidth = 8
	policy.WideLabelWidth = 8
	policy.MinLabelWidth = 4
	return toolkitviews.NewKeyValueActionRowWithPolicy[state.Model](label, value, verb+" ↵", policy, onActivate)
}

func RowActionWideValueWithCancel(label, value, verb string, onActivate func() []update.Action, onCancel func() []update.Action) retained.ViewSpec[state.Model] {
	policy := cockpitRowPolicy()
	policy.StandardLabelWidth = 8
	policy.WideLabelWidth = 8
	policy.MinLabelWidth = 4
	return toolkitviews.NewKeyValueActionRowWithCancelAndPolicy[state.Model](label, value, verb+" ↵", policy, onActivate, onCancel)
}

func RowActionWithCancel(label, value, verb string, onActivate func() []update.Action, onCancel func() []update.Action) retained.ViewSpec[state.Model] {
	return RowKVWithCancel(label, value, verb+" ↵", onActivate, onCancel)
}

func RowActionWithHooks(label, value, verb string, onActivate func() []update.Action, onCancel func() []update.Action, onFocus func() []update.Action) retained.ViewSpec[state.Model] {
	return RowKVWithHooks(label, value, verb+" ↵", onActivate, onCancel, onFocus)
}

func RowChoiceWithHooks(label, value string, onActivate func() []update.Action, onCancel func() []update.Action, onFocus func() []update.Action) retained.ViewSpec[state.Model] {
	return RowKVWithHooks(label, value, "choose ↵", onActivate, onCancel, onFocus)
}

func RowChoiceWithCancel(label, value string, onActivate func() []update.Action, onCancel func() []update.Action) retained.ViewSpec[state.Model] {
	return RowKVWithCancel(label, value, "choose ↵", onActivate, onCancel)
}

func RowManageWithHooks(label, value string, onActivate func() []update.Action, onCancel func() []update.Action, onFocus func() []update.Action) retained.ViewSpec[state.Model] {
	return RowKVWithHooks(label, value, "manage ↵", onActivate, onCancel, onFocus)
}

func RowEditWithHooks(label, value string, onActivate func() []update.Action, onCancel func() []update.Action, onFocus func() []update.Action) retained.ViewSpec[state.Model] {
	return RowKVWithHooks(label, value, "edit ↵", onActivate, onCancel, onFocus)
}

func RowEditWithCancel(label, value string, onActivate func() []update.Action, onCancel func() []update.Action) retained.ViewSpec[state.Model] {
	return RowKVWithCancel(label, value, "edit ↵", onActivate, onCancel)
}

func InlineEditor(label, value, emptyValue string, onChange func(string) []update.Action, onCommit func(string) []update.Action, onCancel func() []update.Action) retained.ViewSpec[state.Model] {
	return toolkitviews.NewInlineEditor[state.Model](label, value, emptyValue, cockpitRowPolicy(), onChange, onCommit, onCancel)
}
