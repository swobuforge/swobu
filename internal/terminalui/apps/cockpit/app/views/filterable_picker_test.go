package views

import (
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
)

func TestHandleFilterableEsc_ClearsQueryBeforeClose(t *testing.T) {
	setCalled := false
	var gotState FilterablePickerState
	cancelCalled := false
	handled, actions := handleFilterableEsc(
		FilterablePickerState{Query: "claude", Cursor: 2, Offset: 1},
		func(next FilterablePickerState) {
			setCalled = true
			gotState = next
		},
		[]FilterablePickerItem{{Label: "claude-sonnet-4-20250514"}},
		FilterablePickerConfig{
			KeyPrefix: "provider-model-option",
			OnCancel: func() []update.Action {
				cancelCalled = true
				return []update.Action{interaction.FocusKeyAction{Key: "model"}}
			},
		},
	)
	if !handled {
		t.Fatal("esc should be handled")
	}
	if !setCalled {
		t.Fatal("esc with non-empty query must reset picker state")
	}
	if gotState.Query != "" || gotState.Cursor != 0 || gotState.Offset != 0 {
		t.Fatalf("state=%+v want query=\"\" cursor=0 offset=0", gotState)
	}
	if cancelCalled {
		t.Fatal("first esc with non-empty query must not close picker")
	}
	if len(actions) == 0 {
		t.Fatal("expected focus action after clearing query")
	}
}

func TestHandleFilterableEsc_ClosesWhenQueryEmpty(t *testing.T) {
	setCalled := false
	cancelCalled := false
	handled, actions := handleFilterableEsc(
		FilterablePickerState{},
		func(FilterablePickerState) { setCalled = true },
		nil,
		FilterablePickerConfig{
			OnCancel: func() []update.Action {
				cancelCalled = true
				return []update.Action{interaction.FocusKeyAction{Key: "model"}}
			},
		},
	)
	if !handled {
		t.Fatal("esc should be handled")
	}
	if setCalled {
		t.Fatal("empty-query esc must not reset state")
	}
	if !cancelCalled {
		t.Fatal("empty-query esc must close picker via OnCancel")
	}
	if len(actions) != 1 {
		t.Fatalf("actions len=%d want 1", len(actions))
	}
}

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

func TestHandleFilterableKeyUp_AtTopClosesPickerViaOnCancel(t *testing.T) {
	t.Parallel()
	cancelCalled := false
	handled, actions := handleFilterableKeyUp(
		FilterablePickerState{Cursor: 0},
		func(FilterablePickerState) {},
		[]FilterablePickerItem{{Label: "a"}},
		FilterablePickerConfig{
			OnCancel: func() []update.Action {
				cancelCalled = true
				return []update.Action{interaction.FocusKeyAction{Key: "credential"}}
			},
		},
	)
	if !handled {
		t.Fatal("expected handled=true")
	}
	if !cancelCalled {
		t.Fatal("expected OnCancel call at top-boundary key up")
	}
	if len(actions) != 1 {
		t.Fatalf("actions len=%d want 1", len(actions))
	}
}

func TestHandleFilterableKeyDown_AtBottomClosesPickerViaOnCancel(t *testing.T) {
	t.Parallel()
	cancelCalled := false
	handled, actions := handleFilterableKeyDown(
		FilterablePickerState{Cursor: 1},
		func(FilterablePickerState) {},
		[]FilterablePickerItem{{Label: "a"}, {Label: "b"}},
		FilterablePickerConfig{
			OnCancel: func() []update.Action {
				cancelCalled = true
				return []update.Action{interaction.FocusKeyAction{Key: "credential"}}
			},
		},
	)
	if !handled {
		t.Fatal("expected handled=true")
	}
	if !cancelCalled {
		t.Fatal("expected OnCancel call at bottom-boundary key down")
	}
	if len(actions) != 1 {
		t.Fatalf("actions len=%d want 1", len(actions))
	}
}

func TestHandleFilterableKeyUp_WithEarlierItems_DoesNotCloseAndMovesWithinPicker(t *testing.T) {
	t.Parallel()
	cancelCalled := false
	var got FilterablePickerState
	handled, actions := handleFilterableKeyUp(
		FilterablePickerState{Cursor: 3, Offset: 3},
		func(next FilterablePickerState) { got = next },
		[]FilterablePickerItem{{Label: "a"}, {Label: "b"}, {Label: "c"}, {Label: "d"}, {Label: "e"}},
		FilterablePickerConfig{
			KeyPrefix: "opt",
			OnCancel: func() []update.Action {
				cancelCalled = true
				return nil
			},
		},
	)
	if !handled {
		t.Fatal("expected handled=true")
	}
	if cancelCalled {
		t.Fatal("must not close while picker can still move upward")
	}
	if got.Cursor != 2 || got.Offset != 2 {
		t.Fatalf("state=%+v want cursor=2 offset=2", got)
	}
	if len(actions) != 1 {
		t.Fatalf("actions len=%d want 1", len(actions))
	}
}

func TestHandleFilterableKeyDown_WithLaterItems_DoesNotCloseAndMovesWithinPicker(t *testing.T) {
	t.Parallel()
	cancelCalled := false
	var got FilterablePickerState
	handled, actions := handleFilterableKeyDown(
		FilterablePickerState{Cursor: 1, Offset: 0},
		func(next FilterablePickerState) { got = next },
		[]FilterablePickerItem{{Label: "a"}, {Label: "b"}, {Label: "c"}, {Label: "d"}, {Label: "e"}},
		FilterablePickerConfig{
			KeyPrefix:  "opt",
			WindowSize: 2,
			OnCancel: func() []update.Action {
				cancelCalled = true
				return nil
			},
		},
	)
	if !handled {
		t.Fatal("expected handled=true")
	}
	if cancelCalled {
		t.Fatal("must not close while picker can still move downward")
	}
	if got.Cursor != 2 || got.Offset != 1 {
		t.Fatalf("state=%+v want cursor=2 offset=1", got)
	}
	if len(actions) != 1 {
		t.Fatalf("actions len=%d want 1", len(actions))
	}
}
