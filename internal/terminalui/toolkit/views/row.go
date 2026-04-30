// Row views compose toolkit views for label/value/action records.
package views

import (
	"strings"

	"github.com/metrofun/swobu/internal/terminalui/engine/retained/update"
	"github.com/metrofun/swobu/internal/terminalui/engine/retained/view"
)

type rowViewSpec[M any] struct {
	label      string
	value      string
	action     string
	policy     RowLayoutPolicy
	onActivate func() []update.Action
	onCancel   func() []update.Action
	onFocus    func() []update.Action
}

func rowViewSpecNode[M any](r rowViewSpec[M]) view.RenderNode {
	actionable := strings.TrimSpace(r.action) != "" && r.onActivate != nil
	allowSpace := strings.Contains(strings.ToLower(strings.TrimSpace(r.action)), "toggle")
	parts := newRowParts(r.label, r.value, r.action, false)
	policy := r.policy
	if policy.MaxLabelFractionDiv <= 0 {
		policy = DefaultRowLayoutPolicy()
	}
	el := NewAction(parts.intrinsicWidth(), actionable, allowSpace, func(focused bool, width int) string {
		return newRowParts(r.label, r.value, r.action, focused && actionable).render(width, policy)
	}, func(string) []update.Action {
		if r.onActivate == nil {
			return nil
		}
		return r.onActivate()
	}, r.onCancel)
	el.OnFocusAction = r.onFocus
	return el
}

func newRowViewSpec[M any](r rowViewSpec[M]) view.ViewSpec[M] {
	return view.View[M](func(_ *view.Context[M]) view.RenderNode {
		return rowViewSpecNode(r)
	})
}

func NewKeyValueActionRow[M any](label, value, action string, onActivate func() []update.Action) view.ViewSpec[M] {
	return NewKeyValueActionRowWithPolicy[M](label, value, action, DefaultRowLayoutPolicy(), onActivate)
}

func NewKeyValueActionRowWithCancel[M any](label, value, action string, onActivate func() []update.Action, onCancel func() []update.Action) view.ViewSpec[M] {
	return NewKeyValueActionRowWithCancelAndPolicy[M](label, value, action, DefaultRowLayoutPolicy(), onActivate, onCancel)
}

func NewKeyValueActionRowWithHooks[M any](label, value, action string, onActivate func() []update.Action, onCancel func() []update.Action, onFocus func() []update.Action) view.ViewSpec[M] {
	return NewKeyValueActionRowWithHooksAndPolicy[M](label, value, action, DefaultRowLayoutPolicy(), onActivate, onCancel, onFocus)
}

func NewKeyValueActionRowWithPolicy[M any](label, value, action string, policy RowLayoutPolicy, onActivate func() []update.Action) view.ViewSpec[M] {
	return newRowViewSpec(rowViewSpec[M]{label: label, value: value, action: action, policy: policy, onActivate: onActivate})
}

func NewKeyValueActionRowWithCancelAndPolicy[M any](label, value, action string, policy RowLayoutPolicy, onActivate func() []update.Action, onCancel func() []update.Action) view.ViewSpec[M] {
	return newRowViewSpec(rowViewSpec[M]{label: label, value: value, action: action, policy: policy, onActivate: onActivate, onCancel: onCancel})
}

func NewKeyValueActionRowWithHooksAndPolicy[M any](label, value, action string, policy RowLayoutPolicy, onActivate func() []update.Action, onCancel func() []update.Action, onFocus func() []update.Action) view.ViewSpec[M] {
	return newRowViewSpec(rowViewSpec[M]{label: label, value: value, action: action, policy: policy, onActivate: onActivate, onCancel: onCancel, onFocus: onFocus})
}

func NewStaticValueRow[M any](label, value string) view.ViewSpec[M] {
	return NewKeyValueActionRow[M](label, value, "", nil)
}

func NewActionRow[M any](label, value, verb string, onActivate func() []update.Action) view.ViewSpec[M] {
	return NewKeyValueActionRow[M](label, value, verb+" ↵", onActivate)
}

func NewActionRowWithCancel[M any](label, value, verb string, onActivate func() []update.Action, onCancel func() []update.Action) view.ViewSpec[M] {
	return NewKeyValueActionRowWithCancel[M](label, value, verb+" ↵", onActivate, onCancel)
}

func NewActionRowWithHooks[M any](label, value, verb string, onActivate func() []update.Action, onCancel func() []update.Action, onFocus func() []update.Action) view.ViewSpec[M] {
	return NewKeyValueActionRowWithHooks[M](label, value, verb+" ↵", onActivate, onCancel, onFocus)
}

func NewChoiceRow[M any](label, value string, onActivate func() []update.Action) view.ViewSpec[M] {
	return NewKeyValueActionRow[M](label, value, "choose ↵", onActivate)
}

func NewChoiceRowWithCancel[M any](label, value string, onActivate func() []update.Action, onCancel func() []update.Action) view.ViewSpec[M] {
	return NewKeyValueActionRowWithCancel[M](label, value, "choose ↵", onActivate, onCancel)
}

func NewChoiceRowWithHooks[M any](label, value string, onActivate func() []update.Action, onCancel func() []update.Action, onFocus func() []update.Action) view.ViewSpec[M] {
	return NewKeyValueActionRowWithHooks[M](label, value, "choose ↵", onActivate, onCancel, onFocus)
}

func NewManageRow[M any](label, value string, onActivate func() []update.Action) view.ViewSpec[M] {
	return NewKeyValueActionRow[M](label, value, "manage ↵", onActivate)
}

func NewManageRowWithCancel[M any](label, value string, onActivate func() []update.Action, onCancel func() []update.Action) view.ViewSpec[M] {
	return NewKeyValueActionRowWithCancel[M](label, value, "manage ↵", onActivate, onCancel)
}

func NewManageRowWithHooks[M any](label, value string, onActivate func() []update.Action, onCancel func() []update.Action, onFocus func() []update.Action) view.ViewSpec[M] {
	return NewKeyValueActionRowWithHooks[M](label, value, "manage ↵", onActivate, onCancel, onFocus)
}

func NewEditRow[M any](label, value string, onActivate func() []update.Action) view.ViewSpec[M] {
	return NewKeyValueActionRow[M](label, value, "edit ↵", onActivate)
}

func NewEditRowWithCancel[M any](label, value string, onActivate func() []update.Action, onCancel func() []update.Action) view.ViewSpec[M] {
	return NewKeyValueActionRowWithCancel[M](label, value, "edit ↵", onActivate, onCancel)
}

func NewEditRowWithHooks[M any](label, value string, onActivate func() []update.Action, onCancel func() []update.Action, onFocus func() []update.Action) view.ViewSpec[M] {
	return NewKeyValueActionRowWithHooks[M](label, value, "edit ↵", onActivate, onCancel, onFocus)
}

func NewToggleRow[M any](label string, enabled bool, onActivate func() []update.Action) view.ViewSpec[M] {
	v := "off"
	if enabled {
		v = "on"
	}
	return NewKeyValueActionRow[M](label, v, "toggle space", onActivate)
}

func NewEvidenceRow[M any](requestID, operation, target, timing, result string, onActivate func() []update.Action) view.ViewSpec[M] {
	parts := []string{strings.TrimSpace(target), strings.TrimSpace(timing), strings.TrimSpace(result), strings.TrimSpace(operation)}
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
	return NewKeyValueActionRow[M](strings.TrimSpace(requestID), strings.Join(filtered, "   "), action, onActivate)
}

// --- Row layout internals ---

type rowParts struct {
	Marker string
	Label  string
	Value  string
	Action string
}

func newRowParts(label, value, action string, focused bool) rowParts {
	marker := " "
	if focused && strings.TrimSpace(action) != "" {
		marker = ">"
	}
	return rowParts{
		Marker: marker,
		Label:  strings.TrimSpace(label),
		Value:  strings.TrimSpace(value),
		Action: strings.TrimSpace(action),
	}
}

func (r rowParts) intrinsicWidth() int {
	return runeLen(r.Marker) + runeLen(r.Label) + runeLen(r.Value) + runeLen(r.Action) + 6
}

type RowLayoutPolicy struct {
	WideWidthThreshold  int
	StandardLabelWidth  int
	WideLabelWidth      int
	MinLabelWidth       int
	MaxLabelFractionDiv int
	ActionStartCol      int
}

func DefaultRowLayoutPolicy() RowLayoutPolicy {
	return RowLayoutPolicy{
		WideWidthThreshold:  100,
		StandardLabelWidth:  17,
		WideLabelWidth:      18,
		MinLabelWidth:       8,
		MaxLabelFractionDiv: 3,
		ActionStartCol:      0,
	}
}

func (r rowParts) render(width int, policy RowLayoutPolicy) string {
	if width <= 0 {
		return ""
	}
	if policy.MaxLabelFractionDiv <= 0 {
		policy = DefaultRowLayoutPolicy()
	}
	actionWidth := runeLen(r.Action)
	if actionWidth > width/policy.MaxLabelFractionDiv {
		actionWidth = width / policy.MaxLabelFractionDiv
	}
	labelWidth := policy.StandardLabelWidth
	if width >= policy.WideWidthThreshold {
		labelWidth = policy.WideLabelWidth
	}
	maxLabelWidth := max(policy.MinLabelWidth, width/policy.MaxLabelFractionDiv)
	if labelWidth > maxLabelWidth {
		labelWidth = maxLabelWidth
	}
	remaining := width - 5 - labelWidth
	if actionWidth > 0 {
		remaining -= 1 + actionWidth
	}
	if remaining < 0 {
		remaining = 0
	}
	line := r.Marker + "   " + padRight(trimToWidth(r.Label, labelWidth), labelWidth) + " "
	if remaining > 0 {
		line += trimToWidth(r.Value, remaining)
	}
	if actionWidth > 0 {
		actionStartCol := policy.ActionStartCol
		if actionStartCol <= 0 {
			line = padRight(trimToWidth(line, max(0, width-1-actionWidth)), max(0, width-1-actionWidth))
			line += " " + trimToWidth(r.Action, actionWidth)
			return padRight(trimToWidth(line, width), width)
		}
		prefixWidth := actionStartCol - 1
		if prefixWidth > width-actionWidth {
			prefixWidth = max(0, width-actionWidth)
		}
		line = padRight(trimToWidth(line, prefixWidth), prefixWidth)
		line += trimToWidth(r.Action, actionWidth)
	}
	return padRight(trimToWidth(line, width), width)
}
