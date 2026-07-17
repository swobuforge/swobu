package ui

import (
	"testing"

	tui "github.com/grindlemire/go-tui"
)

func TestSelectableRow_RenderDoesNotAutofocus(t *testing.T) {
	row := NewSelectableRow("row.autofocus", "label", "value", "select ↵", nil)
	row.AutoFocus = true

	row.Render(nil)

	if row.IsFocused() {
		t.Fatal("SelectableRow.Render must not perform autofocus repair")
	}
}

func TestChoiceRow_RenderDoesNotAutofocus(t *testing.T) {
	list := NewChoiceList(tui.NewState(""))
	row := NewChoiceRow("choice.autofocus", list, ChoiceRowModel{
		Item: ChoiceItem{Key: "provider", Label: "Provider", Action: "select ↵"},
	}, true)

	row.Render(nil)

	if row.IsFocused() {
		t.Fatal("ChoiceRow.Render must not perform autofocus repair")
	}
}

func TestEditableRow_RenderDoesNotAutofocus(t *testing.T) {
	row := NewEditableRow("editable.autofocus", "name", tui.NewState(""))
	row.AutoFocus = true

	row.Render(nil)

	if row.IsFocused() {
		t.Fatal("EditableRow.Render must not perform autofocus repair")
	}
}
