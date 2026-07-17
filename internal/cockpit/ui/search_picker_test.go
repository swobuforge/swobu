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
	picker.Query.Set("azure ai")

	filtered := picker.filteredOptions()
	if len(filtered) != 1 {
		t.Fatalf("filtered count = %d, want 1", len(filtered))
	}
	if filtered[0].Label != "Azure AI" {
		t.Fatalf("match = %q, want Azure AI", filtered[0].Label)
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

func TestSearchPicker_UpdatePropsPreservesQuery(t *testing.T) {
	picker := samplePicker()
	picker.Query.Set("open")

	fresh := NewSearchPicker("providers", "provider", []SearchOption{
		{ID: "openai", Label: "OpenAI"},
	}, nil, nil)
	picker.UpdateProps(fresh)

	if got := picker.Query.Get(); got != "open" {
		t.Fatalf("query after option replacement = %q, want open", got)
	}
	if len(picker.Options) != 1 {
		t.Fatalf("options after update = %d, want 1", len(picker.Options))
	}
}

func TestSearchPicker_OptionComponentIDIncludesPickerID(t *testing.T) {
	picker := NewSearchPicker("provider-picker", "provider", sampleOptions(), nil, nil)
	list := NewChoiceList(tui.NewState(""))
	row := ChoiceRowModel{Item: ChoiceItem{Key: "openai", Label: "OpenAI"}}

	option := SearchPickerOptionComponent(picker, list, row, false)

	if got, want := option.target.Props().ID, "provider-picker:option:openai"; got != want {
		t.Fatalf("option component ID = %q, want %q", got, want)
	}
}

func TestSearchPicker_UpdatePropsRefreshesPickerID(t *testing.T) {
	picker := NewSearchPicker("provider-picker", "provider", sampleOptions(), nil, nil)
	fresh := NewSearchPicker("model-picker", "model", sampleOptions(), nil, nil)

	picker.UpdateProps(fresh)

	if got := picker.ID; got != "model-picker" {
		t.Fatalf("picker ID after update = %q, want model-picker", got)
	}
}

func TestSearchPicker_RenderBoundsVisibleRows(t *testing.T) {
	picker := NewSearchPicker("test", "title", manyOptions(112), nil, nil)
	picker.AutoFocus = true

	rendered := testkit.RenderMountedTrimmed(t, picker, 120, 24)

	if got, want := strings.Count(rendered, "Provider "), 7; got != want {
		t.Fatalf("visible provider rows = %d, want %d\n%s", got, want, rendered)
	}
	if !strings.Contains(rendered, "> Provider 0") {
		t.Fatalf("first option should be selected through global selection:\n%s", rendered)
	}
	if !strings.Contains(rendered, "7 of 112 shown") {
		t.Fatalf("footer missing count:\n%s", rendered)
	}
}

func TestSearchPicker_RenderDoesNotRepairWindowStart(t *testing.T) {
	picker := NewSearchPicker("test", "title", manyOptions(8), nil, nil)
	h, err := testkit.NewHarness(picker)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	list := picker.choiceList()
	list.VisibleRows = 3
	list.SetItems(choiceListItems(8))
	list.WindowStart.Set(5)
	list.Items = choiceListItems(4)

	rendered := h.Frame()

	if got, want := list.WindowStart.Get(), 5; got != want {
		t.Fatalf("window start after render = %d, want %d\n%s", got, want, rendered)
	}
	if !strings.Contains(rendered, "Item 1") {
		t.Fatalf("render should still use bounded projection:\n%s", rendered)
	}
}

func TestSearchPicker_RenderCountUsesFullSet(t *testing.T) {
	options := manyOptions(112)
	options[10].Keywords = []string{"unique-provider-ten"}
	picker := NewSearchPicker("test", "provider", options, nil, nil)
	picker.Query.Set("unique-provider-ten")

	rendered := testkit.RenderMountedTrimmed(t, picker, 120, 20)

	if !strings.Contains(rendered, "Provider 10") {
		t.Fatalf("filtered option missing:\n%s", rendered)
	}
	if !strings.Contains(rendered, "1 of 112 shown") {
		t.Fatalf("footer should report the match against the full backing set:\n%s", rendered)
	}
	if strings.Contains(rendered, "1 of 1 shown") {
		t.Fatalf("footer should not collapse the denominator to the filtered count:\n%s", rendered)
	}
}

func TestSearchPicker_RenderShowsNoMatchFooter(t *testing.T) {
	picker := samplePicker()
	picker.Query.Set("xyz")

	rendered := testkit.RenderMountedTrimmed(t, picker, 120, 20)

	if !strings.Contains(rendered, "0 of 8 shown") {
		t.Fatalf("no-match footer should report zero of the full backing set:\n%s", rendered)
	}
	if strings.Contains(rendered, "select ↵") {
		t.Fatalf("no-match state should not render selectable options:\n%s", rendered)
	}
}

func TestSearchPicker_RenderAlignsActionHints(t *testing.T) {
	picker := NewSearchPicker("test", "title", []SearchOption{
		{ID: "openai", Label: "OpenAI"},
		{ID: "chatgpt", Label: "ChatGPT"},
		{ID: "anthropic", Label: "Anthropic"},
	}, nil, nil)
	picker.AutoFocus = true

	rendered := testkit.RenderMountedTrimmed(t, picker, 120, 20)
	baseline := testkit.RenderMountedTrimmed(t,
		NewSelectableRow("baseline", "", "value", "select ↵", nil),
		120,
		3,
	)
	baselineColumn := strings.Index(baseline, "select ↵")
	if baselineColumn < 0 {
		t.Fatalf("baseline row missing action hint:\n%s", baseline)
	}

	lines := strings.Split(rendered, "\n")
	checked := 0
	for _, line := range lines {
		hint := "select ↵"
		if strings.Contains(line, "3 of 3 shown") {
			hint = "↑↓ search"
		} else if !strings.Contains(line, "select ↵") {
			continue
		}
		column := strings.Index(line, hint)
		if column < 0 {
			t.Fatalf("missing %q in line %q\n%s", hint, line, rendered)
		}
		if column != baselineColumn {
			t.Fatalf("hint column = %d, want selectable row column %d in line %q\n%s", column, baselineColumn, line, rendered)
		}
		checked++
	}
	if checked != 4 {
		t.Fatalf("checked hint rows = %d, want 4\n%s", checked, rendered)
	}
	if !strings.Contains(rendered, "search            _") {
		t.Fatalf("search query row missing aligned caret:\n%s", rendered)
	}
}

func TestSearchPicker_AppLoop_KeyDownMovesGlobalSelectionToNextOption(t *testing.T) {
	picker := NewSearchPicker("test", "title", sampleOptions(), nil, nil)
	picker.AutoFocus = true
	h, err := testkit.NewHarness(picker)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})

	frame := h.Frame()
	if !strings.Contains(frame, "> ChatGPT") {
		t.Fatalf("Down should select second option through global selection:\n%s", frame)
	}
}

func TestSearchPicker_AppLoop_KeyDownAtVisibleEdgeShiftsProjection(t *testing.T) {
	picker := NewSearchPicker("test", "title", manyOptions(112), nil, nil)
	picker.AutoFocus = true
	h, err := testkit.NewHarness(picker)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	for i := 0; i < 7; i++ {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	}

	frame := h.Frame()
	if !strings.Contains(frame, "> Provider 7") {
		t.Fatalf("Down at the visible edge should focus the next projected row:\n%s", frame)
	}
	if strings.Contains(frame, "Provider 0") {
		t.Fatalf("Down at the visible edge should shift the bounded projection:\n%s", frame)
	}
	if got, want := strings.Count(frame, "Provider "), 7; got != want {
		t.Fatalf("visible provider rows = %d, want %d\n%s", got, want, frame)
	}
}

func TestSearchPicker_AppLoop_PageDownMovesBoundedProjection(t *testing.T) {
	picker := NewSearchPicker("test", "title", manyOptions(112), nil, nil)
	picker.AutoFocus = true
	root := &searchPickerFallbackRoot{picker: picker}
	h, err := testkit.NewHarness(root)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyPageDown})

	frame := h.Frame()
	if !strings.Contains(frame, "> Provider 7") {
		t.Fatalf("PageDown should select the next bounded page:\n%s", frame)
	}
	if strings.Contains(frame, "Provider 0") {
		t.Fatalf("PageDown should advance the list projection before body scroll:\n%s", frame)
	}
	if root.pageDowns != 0 {
		t.Fatalf("parent PageDown fallback calls = %d, want 0", root.pageDowns)
	}
	if !strings.Contains(frame, "7 of 112 shown") {
		t.Fatalf("bounded count should remain stable after PageDown:\n%s", frame)
	}
}

func TestSearchPicker_AppLoop_PageUpMovesBoundedProjection(t *testing.T) {
	picker := NewSearchPicker("test", "title", manyOptions(112), nil, nil)
	picker.AutoFocus = true
	h, err := testkit.NewHarness(picker)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyPageDown})

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyPageUp})

	frame := h.Frame()
	if !strings.Contains(frame, "> Provider 0") {
		t.Fatalf("PageUp should return to the previous bounded page:\n%s", frame)
	}
	if strings.Contains(frame, "Provider 7") {
		t.Fatalf("PageUp should move the list projection back:\n%s", frame)
	}
	if !strings.Contains(frame, "7 of 112 shown") {
		t.Fatalf("bounded count should remain stable after PageUp:\n%s", frame)
	}
}

func TestSearchPicker_AppLoop_KeyEnterSelectsFocusedOption(t *testing.T) {
	var selected string
	picker := NewSearchPicker("test", "title", sampleOptions(), func(sel Selection) { selected = sel.Value }, nil)
	picker.AutoFocus = true
	h, err := testkit.NewHarness(picker)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	if selected != "chatgpt" {
		t.Fatalf("selected = %q, want chatgpt", selected)
	}
}

func TestSearchPicker_AppLoop_TypingFiltersOptions(t *testing.T) {
	picker := NewSearchPicker("test", "title", sampleOptions(), nil, nil)
	picker.AutoFocus = true
	h, err := testkit.NewHarness(picker)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'a'})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'z'})

	frame := h.Frame()
	if !strings.Contains(frame, "search            az_") {
		t.Fatalf("typing should update picker query:\n%s", frame)
	}
	if !strings.Contains(frame, "> Azure AI") {
		t.Fatalf("filtered first option should receive selection:\n%s", frame)
	}
	if strings.Contains(frame, "OpenAI") {
		t.Fatalf("filtered picker should not show unmatched options:\n%s", frame)
	}
}

func TestSearchPicker_AppLoop_FilterKeepsFocusedOptionWhenStillVisible(t *testing.T) {
	picker := NewSearchPicker("test", "title", sampleOptions(), nil, nil)
	h, err := testkit.NewHarness(picker)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	h.App().FocusNext()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'g'})

	frame := h.Frame()
	if !strings.Contains(frame, "> ChatGPT") {
		t.Fatalf("focused matching option should stay selected after filtering:\n%s", frame)
	}
}

func TestSearchPicker_AppLoop_FilterRepairsRemovedFocusToFirstVisibleOption(t *testing.T) {
	picker := NewSearchPicker("test", "title", sampleOptions(), nil, nil)
	h, err := testkit.NewHarness(picker)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	h.App().FocusNext()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'a'})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'z'})

	frame := h.Frame()
	if !strings.Contains(frame, "> Azure AI") {
		t.Fatalf("removed focused option should repair to first visible option:\n%s", frame)
	}
}

func TestSearchPicker_AppLoop_FilterRepairsNoMatchRowFocus(t *testing.T) {
	picker := NewSearchPicker("test", "title", sampleOptions(), nil, nil)
	h, err := testkit.NewHarness(picker)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	h.App().FocusNext()

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'x'})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'y'})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyRune, Rune: 'z'})

	frame := h.Frame()
	if !strings.Contains(frame, "> (no matches)") {
		t.Fatalf("no-match row should become explicit focused outcome:\n%s", frame)
	}
}

func TestSearchPicker_AppLoop_KeyEscapeCancels(t *testing.T) {
	var cancelled bool
	picker := NewSearchPicker("test", "title", sampleOptions(), nil, func() { cancelled = true })
	picker.AutoFocus = true
	root := &searchPickerFallbackRoot{picker: picker}

	h, err := testkit.NewHarness(root)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()

	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})

	if !cancelled {
		t.Fatal("OnCancel not fired via Escape dispatch")
	}
	if root.escapes != 0 {
		t.Fatalf("parent Escape fallback calls = %d, want 0", root.escapes)
	}
}

func TestSearchPicker_AppLoop_KeyEscapeCancelsWhenVisibleSelectionIsNotFrameworkFocus(t *testing.T) {
	var cancelled bool
	picker := NewSearchPicker("test", "title", sampleOptions(), nil, func() { cancelled = true })
	picker.Query.Set("gpt")
	root := &searchPickerFallbackRoot{
		picker: picker,
		after:  NewSelectableRow("after", "after", "", "", nil),
	}
	root.after.AutoFocus = true

	h, err := testkit.NewHarness(root)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()

	h.Open()
	frame := h.Frame()
	if !strings.Contains(frame, "ChatGPT") {
		t.Fatalf("test did not render picker option:\n%s", frame)
	}
	if !strings.Contains(frame, "> after") {
		t.Fatalf("test did not focus non-picker row:\n%s", frame)
	}
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})

	if !cancelled {
		t.Fatal("OnCancel not fired via active picker Escape dispatch")
	}
	if root.escapes != 0 {
		t.Fatalf("parent Escape fallback calls = %d, want 0", root.escapes)
	}
}

func TestSearchPicker_OpenSet_QueryRowRendersWithValue(t *testing.T) {
	picker := NewSearchPicker("model-picker", "model", nil, nil, nil)
	picker.Mode = SearchPickerOpen
	picker.AutoFocus = true
	picker.Query.Set("glm-5.2")

	rendered := testkit.RenderMountedTrimmed(t, picker, 120, 20)

	if !strings.Contains(rendered, "glm-5.2") {
		t.Fatalf("open-set query row missing the typed value:\n%s", rendered)
	}
	if !strings.Contains(rendered, "use ↵") {
		t.Fatalf("open-set query row should offer 'use ↵':\n%s", rendered)
	}
	if strings.Contains(rendered, "select ↵") {
		t.Fatalf("open-set picker with no backing options should not render listed rows:\n%s", rendered)
	}
	if !strings.Contains(rendered, "0 of 0 shown") {
		t.Fatalf("open-set footer should exclude the virtual query row from the count:\n%s", rendered)
	}
}

func TestSearchPicker_OpenSet_QueryRowPrecedesListedMatches(t *testing.T) {
	picker := NewSearchPicker("model-picker", "model", []SearchOption{
		{ID: "glm-5.1", Label: "glm-5.1"},
		{ID: "glm-4.6", Label: "glm-4.6"},
	}, nil, nil)
	picker.Mode = SearchPickerOpen
	picker.AutoFocus = true
	picker.Query.Set("glm")

	rendered := testkit.RenderMountedTrimmed(t, picker, 120, 20)

	if !strings.Contains(rendered, "use ↵") {
		t.Fatalf("query row should render above listed matches:\n%s", rendered)
	}
	if got := strings.Count(rendered, "select ↵"); got != 2 {
		t.Fatalf("want 2 listed matches rendered, got %d:\n%s", got, rendered)
	}
	if !strings.Contains(rendered, "2 of 2 shown") {
		t.Fatalf("footer should count listed matches only:\n%s", rendered)
	}
}

func TestSearchPicker_OpenSet_DedupWhenQueryEqualsListedValue(t *testing.T) {
	picker := NewSearchPicker("model-picker", "model", []SearchOption{
		{ID: "glm-5.1", Label: "glm-5.1"},
	}, nil, nil)
	picker.Mode = SearchPickerOpen
	picker.AutoFocus = true
	picker.Query.Set("glm-5.1")

	rendered := testkit.RenderMountedTrimmed(t, picker, 120, 20)

	if strings.Contains(rendered, "use ↵") {
		t.Fatalf("query equal to a listed value should not duplicate as a query row:\n%s", rendered)
	}
	if !strings.Contains(rendered, "glm-5.1") || !strings.Contains(rendered, "select ↵") {
		t.Fatalf("listed option should remain the candidate:\n%s", rendered)
	}
}

func TestSearchPicker_OpenSet_EnterOnQueryFiresQuerySelection(t *testing.T) {
	var got Selection
	picker := NewSearchPicker("model-picker", "model", []SearchOption{
		{ID: "glm-5.1", Label: "glm-5.1"},
	}, func(sel Selection) { got = sel }, nil)
	picker.Mode = SearchPickerOpen
	picker.AutoFocus = true
	picker.Query.Set("glm-9-custom")
	h, err := testkit.NewHarness(picker)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	if got.Source != SelectionQuery {
		t.Fatalf("selection source = %v, want SelectionQuery", got.Source)
	}
	if got.Value != "glm-9-custom" {
		t.Fatalf("selection value = %q, want glm-9-custom", got.Value)
	}
}

type searchPickerFallbackRoot struct {
	picker    *SearchPicker
	after     *SelectableRow
	escapes   int
	pageDowns int
}

func (r *searchPickerFallbackRoot) Render(app *tui.App) *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column),
		tui.WithWidthPercent(100.00),
	)
	root.AddChild(app.Mount(r, 0, func() tui.Component { return r.picker }))
	if r.after != nil {
		root.AddChild(app.Mount(r, 1, func() tui.Component { return r.after }))
	}
	return root
}

func (r *searchPickerFallbackRoot) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnStop(tui.KeyEscape, func(tui.KeyEvent) { r.escapes++ }),
		tui.OnStop(tui.KeyPageDown, func(tui.KeyEvent) { r.pageDowns++ }),
	}
}

func samplePicker() *SearchPicker {
	return NewSearchPicker("test", "title", sampleOptions(), nil, nil)
}

func sampleOptions() []SearchOption {
	return []SearchOption{
		{ID: "openai", Label: "OpenAI"},
		{ID: "chatgpt", Label: "ChatGPT", Keywords: []string{"gpt"}},
		{ID: "openrouter", Label: "OpenRouter"},
		{ID: "comp", Label: "OpenAI Compatible"},
		{ID: "azure", Label: "Azure AI"},
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
