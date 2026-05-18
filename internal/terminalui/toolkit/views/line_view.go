// Row views compose toolkit views for label/value/action records.
package views

import (
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

type rowViewSpec[M any] struct {
	label      string
	value      string
	action     string
	policy     LineLayoutPolicy
	onActivate func() []update.Action
	onCancel   func() []update.Action
	onFocus    func() []update.Action
}

func rowViewSpecNode[M any](r rowViewSpec[M]) retained.RenderNode {
	actionable := strings.TrimSpace(r.action) != "" && r.onActivate != nil                 // swobu:io-string source=boundary
	allowSpace := strings.Contains(strings.ToLower(strings.TrimSpace(r.action)), "toggle") // swobu:io-string source=boundary
	parts := newRowParts(r.label, r.value, r.action, false)
	policy := r.policy
	if policy.MaxLabelFractionDiv <= 0 {
		policy = DefaultLineLayoutPolicy()
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

func newRowViewSpec[M any](r rowViewSpec[M]) retained.ViewSpec[M] {
	return retained.View[M](func(_ *retained.Context[M]) retained.RenderNode {
		return rowViewSpecNode(r)
	})
}

func NewKeyValueActionRow[M any](label, value, action string, onActivate func() []update.Action) retained.ViewSpec[M] {
	return NewKeyValueActionRowWithPolicy[M](label, value, action, DefaultLineLayoutPolicy(), onActivate)
}

func NewKeyValueActionRowWithCancel[M any](label, value, action string, onActivate func() []update.Action, onCancel func() []update.Action) retained.ViewSpec[M] {
	return NewKeyValueActionRowWithCancelAndPolicy[M](label, value, action, DefaultLineLayoutPolicy(), onActivate, onCancel)
}

func NewKeyValueActionRowWithHooks[M any](label, value, action string, onActivate func() []update.Action, onCancel func() []update.Action, onFocus func() []update.Action) retained.ViewSpec[M] {
	return NewKeyValueActionRowWithHooksAndPolicy[M](label, value, action, DefaultLineLayoutPolicy(), onActivate, onCancel, onFocus)
}

func NewKeyValueActionRowWithPolicy[M any](label, value, action string, policy LineLayoutPolicy, onActivate func() []update.Action) retained.ViewSpec[M] {
	return newRowViewSpec(rowViewSpec[M]{label: label, value: value, action: action, policy: policy, onActivate: onActivate})
}

func NewKeyValueActionRowWithCancelAndPolicy[M any](label, value, action string, policy LineLayoutPolicy, onActivate func() []update.Action, onCancel func() []update.Action) retained.ViewSpec[M] {
	return newRowViewSpec(rowViewSpec[M]{label: label, value: value, action: action, policy: policy, onActivate: onActivate, onCancel: onCancel})
}

func NewKeyValueActionRowWithHooksAndPolicy[M any](label, value, action string, policy LineLayoutPolicy, onActivate func() []update.Action, onCancel func() []update.Action, onFocus func() []update.Action) retained.ViewSpec[M] {
	return newRowViewSpec(rowViewSpec[M]{label: label, value: value, action: action, policy: policy, onActivate: onActivate, onCancel: onCancel, onFocus: onFocus})
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

// --- Row layout internals ---

type rowParts struct {
	Marker string
	Label  string
	Value  string
	Action string
}

func newRowParts(label, value, action string, focused bool) rowParts {
	marker := " "
	if focused && strings.TrimSpace(action) != "" { // swobu:io-string source=boundary
		marker = ">"
	}
	return rowParts{
		Marker: marker,
		Label:  strings.TrimSpace(label),  // swobu:io-string source=boundary
		Value:  strings.TrimSpace(value),  // swobu:io-string source=boundary
		Action: strings.TrimSpace(action), // swobu:io-string source=boundary
	}
}

func (r rowParts) intrinsicWidth() int {
	return runeLen(r.Marker) + runeLen(r.Label) + runeLen(r.Value) + runeLen(r.Action) + 6
}

type LineLayoutPolicy struct {
	WideWidthThreshold  int
	StandardLabelWidth  int
	WideLabelWidth      int
	MinLabelWidth       int
	MaxLabelFractionDiv int
	ActionColumnWidth   int
}

const rowMinContentWidthForAction = 12

func DefaultLineLayoutPolicy() LineLayoutPolicy {
	return LineLayoutPolicy{
		WideWidthThreshold:  100,
		StandardLabelWidth:  17,
		WideLabelWidth:      18,
		MinLabelWidth:       8,
		MaxLabelFractionDiv: 3,
		ActionColumnWidth:   10,
	}
}

func (r rowParts) render(width int, policy LineLayoutPolicy) string {
	if width <= 0 {
		return ""
	}
	if policy.MaxLabelFractionDiv <= 0 {
		policy = DefaultLineLayoutPolicy()
	}
	labelWidth := policy.StandardLabelWidth
	if width >= policy.WideWidthThreshold {
		labelWidth = policy.WideLabelWidth
	}
	maxLabelWidth := max(policy.MinLabelWidth, width/policy.MaxLabelFractionDiv)
	if labelWidth > maxLabelWidth {
		labelWidth = maxLabelWidth
	}
	actionBasis := max(0, policy.ActionColumnWidth)
	items := []InlineItemSpec{
		{
			Text:     r.Marker + "   ",
			Basis:    4,
			Grow:     0,
			Shrink:   0,
			Min:      4,
			Priority: OverflowPreserve,
		},
		{
			Text:     r.Label,
			Basis:    labelWidth,
			Grow:     0,
			Shrink:   1,
			Min:      policy.MinLabelWidth,
			Priority: OverflowPreserve,
		},
		{
			Text:     r.Value,
			Basis:    runeLen(r.Value),
			Grow:     1,
			Shrink:   1,
			Min:      0,
			Priority: OverflowNormal,
		},
	}
	if strings.TrimSpace(r.Action) != "" { // swobu:io-string source=boundary
		items = append(items, InlineItemSpec{
			Text:     r.Action,
			Basis:    actionBasis,
			Grow:     0,
			Shrink:   1,
			Min:      0,
			Priority: OverflowSacrifice,
			// Action slot is anchored to the right by preceding growable content,
			// but text remains left-aligned within the slot for stable scanability.
			AlignRight: false,
		})
	}
	// Keep a fixed right action column only when the left content can still
	// remain readable; otherwise remove action entirely.
	if len(items) == 4 {
		leftMin := items[0].Min + 1 + items[1].Min
		if width < leftMin+1+items[3].Basis || width-1-items[3].Basis < rowMinContentWidthForAction {
			items = items[:3]
		}
	}
	return renderInline(InlineLayoutSpec{Gap: 1, Items: items}, width)
}
