package ui

import (
	"fmt"
	"testing"

	tui "github.com/grindlemire/go-tui"
)

func TestChoiceList_DefaultWindowIsBounded(t *testing.T) {
	list := NewChoiceList(tui.NewState(""))
	list.SetItems(choiceListItems(12))

	win := list.Window()

	if got, want := len(win.Rows), choiceListDefaultVisibleRows; got != want {
		t.Fatalf("default projected rows = %d, want %d", got, want)
	}
	if got, want := win.ShownRows, choiceListDefaultVisibleRows; got != want {
		t.Fatalf("default shown rows = %d, want %d", got, want)
	}
	if got, want := win.TotalRows, 12; got != want {
		t.Fatalf("total rows = %d, want %d", got, want)
	}
}

func TestChoiceList_ShowAllRowsIsExplicit(t *testing.T) {
	list := NewChoiceList(tui.NewState(""))
	list.ShowAllRows = true
	list.SetItems(choiceListItems(12))

	win := list.Window()

	if got, want := len(win.Rows), 12; got != want {
		t.Fatalf("show-all projected rows = %d, want %d", got, want)
	}
	if got, want := win.ShownRows, 12; got != want {
		t.Fatalf("show-all shown rows = %d, want %d", got, want)
	}
}

func TestChoiceList_WindowBoundsProjectionWithoutMutatingWindowStart(t *testing.T) {
	list := NewChoiceList(tui.NewState(""))
	list.VisibleRows = 3
	list.SetItems(choiceListItems(8))
	list.WindowStart.Set(5)

	list.Items = choiceListItems(4)
	win := list.Window()

	if got, want := list.WindowStart.Get(), 5; got != want {
		t.Fatalf("window start after pure window read = %d, want %d", got, want)
	}
	if got, want := len(win.Rows), 3; got != want {
		t.Fatalf("projected rows after shrink = %d, want %d", got, want)
	}
	if got, want := win.Rows[0].Item.Key, "item-1"; got != want {
		t.Fatalf("first projected row after shrink = %q, want %q", got, want)
	}
}

func TestChoiceList_RepairProjectionRepairsStaleWindowStart(t *testing.T) {
	list := NewChoiceList(tui.NewState(""))
	list.VisibleRows = 3
	list.SetItems(choiceListItems(8))
	list.WindowStart.Set(5)
	list.Items = choiceListItems(4)

	list.RepairProjection()

	if got, want := list.WindowStart.Get(), 1; got != want {
		t.Fatalf("window start after repair = %d, want %d", got, want)
	}
}

func TestChoiceList_SetItemsRepairsStaleWindowStart(t *testing.T) {
	list := NewChoiceList(tui.NewState(""))
	list.VisibleRows = 3
	list.SetItems(choiceListItems(8))
	list.WindowStart.Set(5)

	list.SetItems(choiceListItems(4))

	if got, want := list.WindowStart.Get(), 1; got != want {
		t.Fatalf("window start after item update = %d, want %d", got, want)
	}
}

func choiceListItems(n int) []ChoiceItem {
	items := make([]ChoiceItem, 0, n)
	for i := 0; i < n; i++ {
		items = append(items, ChoiceItem{
			Key:   fmt.Sprintf("item-%d", i),
			Label: fmt.Sprintf("Item %d", i),
		})
	}
	return items
}
