package views

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

func TestFilterablePickerItems_FiltersBySearchOrLabel(t *testing.T) {
	items := []FilterablePickerItem{
		{Label: "OpenAI", Search: "openai provider"},
		{Label: "OpenRouter", Search: "openrouter provider"},
		{Label: "Custom"},
	}
	filtered := filterablePickerItems(items, "open")
	if len(filtered) != 2 {
		t.Fatalf("filtered len=%d want 2", len(filtered))
	}
	if filtered[0].Label != "OpenAI" || filtered[1].Label != "OpenRouter" {
		t.Fatalf("filtered labels=%q,%q", filtered[0].Label, filtered[1].Label)
	}

	filtered = filterablePickerItems(items, "cust")
	if len(filtered) != 1 || filtered[0].Label != "Custom" {
		t.Fatalf("filtered openai_compatible mismatch: %#v", filtered)
	}
}

func TestFilterablePickerRows_StickyFindAndWindowMarkers(t *testing.T) {
	items := []FilterablePickerItem{
		{Label: "a"}, {Label: "b"}, {Label: "c"}, {Label: "d"}, {Label: "e"}, {Label: "f"},
	}
	rows, filtered, next := filterablePickerRows(FilterablePickerState{Query: "", Cursor: 4, Offset: 0}, items, FilterablePickerConfig{
		KeyPrefix:  "opt",
		WindowSize: 3,
		FindLabel:  "find",
	})

	if len(filtered) != 6 {
		t.Fatalf("filtered len=%d want 6", len(filtered))
	}
	if next.Offset != 2 {
		t.Fatalf("offset=%d want 2", next.Offset)
	}
	if len(rows) != 6 {
		t.Fatalf("rows len=%d want 6 (find + earlier + 3 items + more)", len(rows))
	}
}

func TestFilterablePickerRows_NoMatches(t *testing.T) {
	rows, filtered, next := filterablePickerRows(FilterablePickerState{Query: "zzz", Cursor: 3, Offset: 9}, []FilterablePickerItem{
		{Label: "openai"},
	}, FilterablePickerConfig{
		FindLabel:      "find",
		NoMatchesLabel: "no files",
	})
	if len(filtered) != 0 {
		t.Fatalf("filtered len=%d want 0", len(filtered))
	}
	if next.Cursor != 0 || next.Offset != 0 {
		t.Fatalf("state cursor=%d offset=%d want 0,0", next.Cursor, next.Offset)
	}
	if len(rows) != 2 {
		t.Fatalf("rows len=%d want 2 (find + no matches)", len(rows))
	}
}

func TestFilterablePickerRows_HidesFindForSmallLists(t *testing.T) {
	rows, _, _ := filterablePickerRows(FilterablePickerState{}, []FilterablePickerItem{
		{Label: "openai"},
		{Label: "openrouter"},
		{Label: "anthropic"},
	}, FilterablePickerConfig{
		WindowSize: 6,
		FindLabel:  "find",
	})
	if len(rows) != 3 {
		t.Fatalf("rows len=%d want 3 (no find row for small list)", len(rows))
	}
}

func TestFilterablePickerRows_ShowsFindAtMinThreshold(t *testing.T) {
	rows, _, _ := filterablePickerRows(FilterablePickerState{}, []FilterablePickerItem{
		{Label: "a"},
		{Label: "b"},
		{Label: "c"},
	}, FilterablePickerConfig{
		WindowSize:        6,
		FindLabel:         "find",
		MinOptionsForFind: 3,
	})
	if len(rows) != 4 {
		t.Fatalf("rows len=%d want 4 (find + 3 options)", len(rows))
	}
}

func TestTrimLastRune(t *testing.T) {
	if got := trimLastRune("ab"); got != "a" {
		t.Fatalf("trimLastRune(ab)=%q want a", got)
	}
	if got := trimLastRune("go🙂"); got != "go" {
		t.Fatalf("trimLastRune(go🙂)=%q want go", got)
	}
	if got := trimLastRune(""); got != "" {
		t.Fatalf("trimLastRune(empty)=%q want empty", got)
	}
}

func TestFocusActionsAfterQueryChange_NoMatchesDoesNotStealFocus(t *testing.T) {
	actions := focusActionsAfterQueryChange([]FilterablePickerItem{
		{Label: "openai"},
	}, FilterablePickerConfig{
		KeyPrefix: "opt",
		OnNoMatchFocus: func() []update.Action {
			return []update.Action{interaction.FocusKeyAction{Key: "outside"}}
		},
	}, "zzz")
	if len(actions) != 0 {
		t.Fatalf("actions len=%d want 0 when query has no matches", len(actions))
	}
}
