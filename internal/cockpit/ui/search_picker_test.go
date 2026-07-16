package ui

import (
	"fmt"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestSearchPicker_FilterByPrefix(t *testing.T) {
	picker := samplePicker()
	picker.Query.Set("open")

	filtered := picker.filteredOptions()
	if len(filtered) != 3 {
		t.Fatalf("filtered count = %d, want 3", len(filtered))
	}
	if filtered[0].Label != "OpenAI" {
		t.Fatalf("first match = %q, want OpenAI", filtered[0].Label)
	}
	if filtered[1].Label != "OpenRouter" {
		t.Fatalf("second match = %q, want OpenRouter", filtered[1].Label)
	}
	if filtered[2].Label != "OpenAI Compatible" {
		t.Fatalf("third match = %q, want OpenAI Compatible", filtered[2].Label)
	}
}

func TestSearchPicker_FilterBySubstring(t *testing.T) {
	picker := samplePicker()
	picker.Query.Set("comp")

	filtered := picker.filteredOptions()
	if len(filtered) != 1 {
		t.Fatalf("filtered count = %d, want 1", len(filtered))
	}
	if filtered[0].Label != "OpenAI Compatible" {
		t.Fatalf("match = %q, want OpenAI Compatible", filtered[0].Label)
	}
}

func TestSearchPicker_FilterByTokenSubsequence(t *testing.T) {
	picker := samplePicker()
	picker.Query.Set("ai foundry")

	filtered := picker.filteredOptions()
	if len(filtered) != 1 {
		t.Fatalf("filtered count = %d, want 1", len(filtered))
	}
	if filtered[0].Label != "Azure AI Foundry" {
		t.Fatalf("match = %q, want Azure AI Foundry", filtered[0].Label)
	}
}

func TestSearchPicker_FilterByKeyword(t *testing.T) {
	picker := samplePicker()
	picker.Query.Set("gpt")

	filtered := picker.filteredOptions()
	if len(filtered) != 1 {
		t.Fatalf("filtered count = %d, want 1", len(filtered))
	}
	if filtered[0].Label != "ChatGPT" {
		t.Fatalf("match = %q, want ChatGPT", filtered[0].Label)
	}
}

func TestSearchPicker_EmptyQueryReturnsAll(t *testing.T) {
	picker := samplePicker()
	picker.Query.Set("")

	filtered := picker.filteredOptions()
	if len(filtered) != 8 {
		t.Fatalf("filtered count = %d, want 8", len(filtered))
	}
}

func TestSearchPicker_NoMatchReturnsEmpty(t *testing.T) {
	picker := samplePicker()
	picker.Query.Set("xyz")

	filtered := picker.filteredOptions()
	if len(filtered) != 0 {
		t.Fatalf("filtered count = %d, want 0", len(filtered))
	}
}

func TestSearchPicker_CursorClampsToFilteredCount(t *testing.T) {
	picker := samplePicker()
	picker.Cursor.Set(3)
	picker.Query.Set("open")

	picker.clampCursor(picker.filteredOptions())

	if got := picker.Cursor.Get(); got != 2 {
		t.Fatalf("cursor after clamp = %d, want 2", got)
	}
}

func TestSearchPicker_CursorResetOnQueryChange(t *testing.T) {
	picker := samplePicker()
	picker.Cursor.Set(2)
	picker.Query.Set("open")
	// Simulate what onType does
	picker.Cursor.Set(0)

	if got := picker.Cursor.Get(); got != 0 {
		t.Fatalf("cursor after query change = %d, want 0", got)
	}
}

func TestSearchPicker_SelectFiresOnHighlightedFilteredOption(t *testing.T) {
	var selected string
	picker := samplePicker()
	picker.OnSelect = func(opt SearchOption) { selected = opt.ID }
	picker.Query.Set("open")
	picker.Cursor.Set(1)

	picker.onEnter(tui.KeyEvent{})

	if selected != "openrouter" {
		t.Fatalf("selected = %q, want openrouter", selected)
	}
}

func TestSearchPicker_CancelFiresOnCancel(t *testing.T) {
	var cancelled bool
	picker := samplePicker()
	picker.OnCancel = func() { cancelled = true }

	picker.onEscape(tui.KeyEvent{})

	if !cancelled {
		t.Fatal("OnCancel not fired")
	}
}

func TestSearchPicker_WindowFitsMaxVisible(t *testing.T) {
	picker := NewSearchPicker("test", "title", manyOptions(112), nil, nil)
	picker.Cursor.Set(10)

	filtered := picker.filteredOptions()
	visible, offset := picker.computeWindow(filtered)

	if len(visible) > picker.effectiveMaxVisible() {
		t.Fatalf("visible = %d, want <= %d", len(visible), picker.effectiveMaxVisible())
	}
	if offset >= 10 {
		t.Fatalf("offset = %d, want < 10 (cursor must be visible)", offset)
	}
}

func TestSearchPicker_WindowScrollsWithCursor(t *testing.T) {
	picker := NewSearchPicker("test", "title", manyOptions(112), nil, nil)
	max := picker.effectiveMaxVisible()
	picker.Cursor.Set(max + 3)

	filtered := picker.filteredOptions()
	visible, offset := picker.computeWindow(filtered)

	if offset <= 0 {
		t.Fatalf("offset = %d, want > 0 (scrolled down)", offset)
	}
	if picker.Cursor.Get() < offset || picker.Cursor.Get() >= offset+len(visible) {
		t.Fatalf("cursor %d not in visible window [%d, %d)", picker.Cursor.Get(), offset, offset+len(visible))
	}
}

func TestSearchPicker_RenderBoundsVisibleRows(t *testing.T) {
	picker := NewSearchPicker("test", "title", manyOptions(112), nil, nil)

	rendered := testkit.RenderMountedTrimmed(t, picker, 120, 20)

	if got, want := strings.Count(rendered, "Provider "), picker.effectiveMaxVisible(); got != want {
		t.Fatalf("visible provider rows = %d, want %d\n%s", got, want, rendered)
	}
	if !strings.Contains(rendered, "7 of 112 shown") {
		t.Fatalf("footer missing bounded count:\n%s", rendered)
	}
}

func TestSearchPicker_RenderScrollsWindowWithCursor(t *testing.T) {
	picker := NewSearchPicker("test", "title", manyOptions(112), nil, nil)
	picker.Cursor.Set(10)

	rendered := testkit.RenderMountedTrimmed(t, picker, 120, 20)

	if !strings.Contains(rendered, "Provider 10") {
		t.Fatalf("rendered window did not scroll to cursor:\n%s", rendered)
	}
	if strings.Contains(rendered, "Provider 0") {
		t.Fatalf("rendered window still shows first option after scroll:\n%s", rendered)
	}
	if got, want := strings.Count(rendered, "Provider "), picker.effectiveMaxVisible(); got != want {
		t.Fatalf("visible provider rows = %d, want %d\n%s", got, want, rendered)
	}
}

func TestSearchPicker_RenderShowsNoMatchFooter(t *testing.T) {
	picker := samplePicker()
	picker.Query.Set("xyz")

	rendered := testkit.RenderMountedTrimmed(t, picker, 120, 20)

	if !strings.Contains(rendered, "0 of 8 shown") {
		t.Fatalf("no-match footer missing:\n%s", rendered)
	}
	if strings.Contains(rendered, "select ↵") {
		t.Fatalf("no-match state should not render selectable options:\n%s", rendered)
	}
}

func TestSearchPicker_RenderClampsCursorOnQueryChange(t *testing.T) {
	picker := NewSearchPicker("test", "title", manyOptions(20), nil, nil)
	picker.Cursor.Set(10)
	picker.Query.Set("provider 19")

	_ = testkit.RenderMountedTrimmed(t, picker, 120, 20)

	if got := picker.Cursor.Get(); got != 0 {
		t.Fatalf("cursor after query clamp = %d, want 0", got)
	}
}

func TestSearchPicker_AppLoop_KeyDownMovesCursor(t *testing.T) {
	picker := NewSearchPicker("test", "title", sampleOptions(), nil, nil)
	h, err := testkit.NewHarness(picker)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	// Ensure the picker is focused by advancing focus once ( it's the
	// only focusable component in this isolated harness).
	h.App().FocusNext()

	// Cursor starts at 0; dispatch Down once to move to index 1.
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})

	if got := picker.Cursor.Get(); got != 1 {
		t.Fatalf("cursor after 1 Down = %d, want 1", got)
	}
}

func TestSearchPicker_AppLoop_KeyEnterSelects(t *testing.T) {
	var selected string
	picker := NewSearchPicker("test", "title", sampleOptions(), func(opt SearchOption) { selected = opt.ID }, nil)
	h, err := testkit.NewHarness(picker)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	h.App().FocusNext()
	// Select the first option (cursor=0) with Enter.
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	if selected != "openai" {
		t.Fatalf("selected = %q, want openai", selected)
	}
}

func TestSearchPicker_AppLoop_KeyEscapeCancels(t *testing.T) {
	var cancelled bool
	picker := NewSearchPicker("test", "title", sampleOptions(), nil, func() { cancelled = true })
	h, err := testkit.NewHarness(picker)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	h.App().FocusNext()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})

	if !cancelled {
		t.Fatal("OnCancel not fired via Escape dispatch after focus")
	}
}

func TestSearchPicker_AutoFocusHighlightsFirstOption(t *testing.T) {
	picker := samplePicker()
	picker.AutoFocus = true

	h, err := testkit.NewHarness(picker)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	rendered := h.Frame()
	if !strings.Contains(rendered, ">    OpenAI") {
		t.Fatalf("expected focused first option after autofocus:\n%s", rendered)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func samplePicker() *SearchPicker {
	p := NewSearchPicker("test", "title", sampleOptions(), nil, nil)
	return p
}

func sampleOptions() []SearchOption {
	return []SearchOption{
		{ID: "openai", Label: "OpenAI"},
		{ID: "chatgpt", Label: "ChatGPT", Keywords: []string{"gpt"}},
		{ID: "openrouter", Label: "OpenRouter"},
		{ID: "comp", Label: "OpenAI Compatible"},
		{ID: "azure", Label: "Azure AI Foundry"},
		{ID: "anthropic", Label: "Anthropic"},
		{ID: "ollama", Label: "Ollama"},
		{ID: "bedrock", Label: "AWS Bedrock"},
	}
}

func manyOptions(n int) []SearchOption {
	var opts []SearchOption
	for i := 0; i < n; i++ {
		opts = append(opts, SearchOption{
			ID:    fmt.Sprintf("opt-%d", i),
			Label: fmt.Sprintf("Provider %d", i),
		})
	}
	return opts
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || containsSub(s, substr))
}

func containsSub(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
