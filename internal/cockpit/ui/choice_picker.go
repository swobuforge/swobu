package ui

import (
	"fmt"

	tui "github.com/grindlemire/go-tui"
)

// ChoiceOption is one value in a closed ChoicePicker set.
type ChoiceOption struct {
	ID    string
	Label string
}

// ChoicePicker presents a closed, non-searchable set of values.
//
// It shares ChoiceList traversal and clipping with searchable controls but
// deliberately owns no query state or text-editing grammar. SelectedValue is
// only the initial focus seed; choosing emits the option value to the parent.
type ChoicePicker struct {
	ID            string
	Options       []ChoiceOption
	SelectedValue string
	OnSelect      func(string)
	OnCancel      func()
	list          *ChoiceList
}

// NewChoicePicker creates the body of an entered Select for a closed value set.
// The parent Select owns the field label and committed value; ChoicePicker
// renders only child choices. selectedValue seeds focus onto the matching
// option; when absent or unmatched, the first option starts selected.
func NewChoicePicker(id string, options []ChoiceOption, selectedValue string, onSelect func(string), onCancel func()) *ChoicePicker {
	picker := &ChoicePicker{
		ID:            id,
		Options:       append([]ChoiceOption(nil), options...),
		SelectedValue: selectedValue,
		OnSelect:      onSelect,
		OnCancel:      onCancel,
		list:          NewChoiceList(nil),
	}
	picker.configureList()
	return picker
}

func (p *ChoicePicker) BindApp(app *tui.App) {
	p.configureList()
	if p.list.FocusKey.Get() == "" {
		p.seedSelection()
	}
	p.list.BindApp(app)
}

func (p *ChoicePicker) UnbindApp() {}

func (p *ChoicePicker) UpdateProps(fresh tui.Component) {
	next, ok := fresh.(*ChoicePicker)
	if !ok {
		return
	}
	selectionChanged := p.SelectedValue != next.SelectedValue
	p.ID = next.ID
	p.Options = append([]ChoiceOption(nil), next.Options...)
	p.SelectedValue = next.SelectedValue
	p.OnSelect = next.OnSelect
	p.OnCancel = next.OnCancel
	p.configureList()
	if selectionChanged || p.list.FocusKey.Get() == "" {
		p.seedSelection()
	}
}

func (p *ChoicePicker) KeyMap() tui.KeyMap {
	return tui.KeyMap{tui.OnPreemptStop(tui.KeyEscape, func(tui.KeyEvent) { p.cancel() })}
}

func (p *ChoicePicker) configureList() {
	if p.list == nil {
		p.list = NewChoiceList(nil)
	}
	p.list.QueryEditing = false
	p.list.OnEscape = func(tui.KeyEvent) { p.cancel() }
	p.list.SetItems(p.choiceItems())
}

func (p *ChoicePicker) choiceItems() []ChoiceItem {
	items := make([]ChoiceItem, 0, len(p.Options))
	for index, option := range p.Options {
		choice := option
		items = append(items, ChoiceItem{
			Key:    choiceOptionKey(choice, index),
			Label:  choice.Label,
			Value:  choice.ID,
			Action: "select ↵",
			Choose: func() {
				if p.OnSelect != nil {
					p.OnSelect(choice.ID)
				}
			},
		})
	}
	return items
}

func choiceOptionKey(option ChoiceOption, index int) string {
	if option.ID != "" {
		return option.ID
	}
	return fmt.Sprintf("option-%d", index)
}

func (p *ChoicePicker) seedSelection() {
	selected := p.SelectedValue
	p.list.RevealFocus(func(item ChoiceItem) bool { return item.Value == selected || item.Key == selected })
}

func (p *ChoicePicker) cancel() {
	if p.OnCancel != nil {
		p.OnCancel()
	}
}

func ChoicePickerOptionComponent(picker *ChoicePicker, row ChoiceRowModel) *ChoiceRow {
	return NewChoiceRow(picker.ID+":option:"+choiceRowKey(row), picker.list, row, false)
}

var (
	_ tui.Component    = (*ChoicePicker)(nil)
	_ tui.KeyListener  = (*ChoicePicker)(nil)
	_ tui.AppBinder    = (*ChoicePicker)(nil)
	_ tui.AppUnbinder  = (*ChoicePicker)(nil)
	_ tui.PropsUpdater = (*ChoicePicker)(nil)
)
