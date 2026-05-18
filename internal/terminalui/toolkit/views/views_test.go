package views

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/geom"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/layout"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/rendergraph/paint"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

func TestKeyValueActionRow_MeasureTracksSharedPresentation(t *testing.T) {
	t.Parallel()

	line := build(NewKeyValueActionRow[struct{}]("daemon", "up", "test enter", noopActions))
	size := line.Measure(geom.Unbounded(), &layout.LayoutContext{})
	want := newRowParts("daemon", "up", "test enter", false).intrinsicWidth()

	if size.W != want || size.H != 1 {
		t.Fatalf("size = (%d,%d), want (%d,1)", size.W, size.H, want)
	}
}

func TestKeyValueActionRow_PaintUsesSharedPresentation(t *testing.T) {
	t.Parallel()

	line := build(NewKeyValueActionRow[struct{}]("daemon", "up", "test enter", noopActions))
	node := &layout.LayoutNode{ID: 7, BorderRect: geom.Rect{W: 40, H: 1}}
	buf := paint.NewBuffer(geom.Rect{W: 40, H: 1})

	line.Paint(buf, node, &layout.PaintContext{FocusedID: 7})

	if got, want := buf.String(), newRowParts("daemon", "up", "test enter", true).render(40, DefaultLineLayoutPolicy()); got != want {
		t.Fatalf("paint = %q, want %q", got, want)
	}
}

func TestKeyValueActionRow_FixedActionColumnFallsBackOnNarrowViewport(t *testing.T) {
	t.Parallel()

	policy := DefaultLineLayoutPolicy()
	line := newRowParts("endpoint", "/c/test/", "copy ↵", true).render(22, policy)
	if !strings.Contains(line, "endpoint") {
		t.Fatalf("render=%q missing label under narrow viewport", line)
	}
	if strings.Contains(line, "copy ↵") {
		t.Fatalf("render=%q should prioritize left content over action under narrow viewport", line)
	}
}

func TestKeyValueActionRow_DropsActionBeforeLeftContentOnTightWidth(t *testing.T) {
	t.Parallel()

	policy := DefaultLineLayoutPolicy()
	line := newRowParts("credential file", "/home/user/openai.key", "browse ↵", true).render(14, policy)
	if strings.Contains(line, "↵") {
		t.Fatalf("render=%q must drop action on tight width", line)
	}
}

func TestKeyValueActionRow_LabelOnlyRowKeepsPrimaryContent(t *testing.T) {
	t.Parallel()

	policy := DefaultLineLayoutPolicy()
	line := newRowParts("delete workspace", "", "delete ↵", false).render(28, policy)
	if strings.TrimSpace(strings.Trim(line, " ")) == "" {
		t.Fatalf("render=%q should not collapse to blank", line)
	}
	if !strings.Contains(line, "delete") {
		t.Fatalf("render=%q missing primary label prefix", line)
	}
	if action := strings.Index(line, "delete ↵"); action >= 0 {
		left := strings.TrimSpace(line[:action])
		if left == "" {
			t.Fatalf("render=%q collapsed to action-only row", line)
		}
	}
}

func TestKeyValueActionRow_ActionAnchoredRightButLeftAligned(t *testing.T) {
	t.Parallel()

	policy := DefaultLineLayoutPolicy()
	line := newRowParts("name", "test", "edit ↵", false).render(44, policy)
	idx := strings.Index(line, "edit ↵")
	if idx < 0 {
		t.Fatalf("render=%q missing action text", line)
	}
	// Anchored right means trailing padding only, no extra lead padding inside action text.
	if !strings.HasSuffix(strings.TrimRight(line, " "), "edit ↵") {
		t.Fatalf("render=%q action should anchor to right edge", line)
	}
}

func TestKeyValueActionRow_ActionColumnHasStableStart(t *testing.T) {
	t.Parallel()

	policy := DefaultLineLayoutPolicy()
	lineA := newRowParts("name", "test", "edit ↵", false).render(64, policy)
	lineB := newRowParts("credential file", "/home/user/openai.key", "browse ↵", false).render(64, policy)
	idxA := strings.Index(lineA, "edit ↵")
	idxB := strings.Index(lineB, "browse ↵")
	if idxA < 0 || idxB < 0 {
		t.Fatalf("missing action text: A=%q B=%q", lineA, lineB)
	}
	if idxA != idxB {
		t.Fatalf("action column misaligned: idxA=%d idxB=%d A=%q B=%q", idxA, idxB, lineA, lineB)
	}
}

func TestChoiceOption_ChooseMarksHandled(t *testing.T) {
	t.Parallel()

	chosen := false
	option := build(NewChoiceOption[struct{}]("openai_compatible", true, func() []update.Action {
		chosen = true
		return nil
	}))

	actions := option.(interaction.EventHandler).HandleEvent(interactionKey(interaction.KeyEnter), nil)
	if !chosen || len(actions) != 0 {
		t.Fatalf("choose = (chosen=%v, actions=%d), want (true, 0)", chosen, len(actions))
	}
}

func TestManageRow_UsesManageVerb(t *testing.T) {
	t.Parallel()

	row := build(NewManageRow[struct{}]("providers", "1 configured", noopActions))
	node := &layout.LayoutNode{ID: 9, BorderRect: geom.Rect{W: 48, H: 1}}
	buf := paint.NewBuffer(geom.Rect{W: 48, H: 1})
	row.Paint(buf, node, &layout.PaintContext{FocusedID: 9})

	if got := buf.String(); !strings.Contains(got, "manage ↵") {
		t.Fatalf("paint = %q, want manage ↵ action", got)
	}
}

func TestInlineEditor_EmitsChangeCommitAndCancel(t *testing.T) {
	t.Parallel()

	var changed string
	var committed string
	cancelled := false
	editor := build(NewInlineEditor[struct{}](
		"name",
		"ac",
		"workspace",
		DefaultLineLayoutPolicy(),
		func(value string) []update.Action { changed = value; return nil },
		func(value string) []update.Action { committed = value; return nil },
		func() []update.Action { cancelled = true; return nil },
	))

	h := editor.(interaction.EventHandler)
	h.HandleEvent(interactionRune('m'), nil)
	if changed != "acm" {
		t.Fatalf("rune change changed=%q, want acm", changed)
	}
	_ = h.HandleEvent(interactionBackspace(), nil)
	if changed != "ac" {
		t.Fatalf("backspace change = %q, want ac", changed)
	}
	_ = h.HandleEvent(interactionKey(interaction.KeyEnter), nil)
	if committed != "ac" {
		t.Fatalf("commit = %q, want ac", committed)
	}
	_ = h.HandleEvent(interactionKey(interaction.KeyEsc), nil)
	if !cancelled {
		t.Fatalf("cancel = %v, want true", cancelled)
	}
}

func TestInlineEditor_PaintUsesSharedRowLayout(t *testing.T) {
	t.Parallel()

	editor := build(NewInlineEditor[struct{}](
		"name",
		"ac",
		"workspace",
		DefaultLineLayoutPolicy(),
		func(string) []update.Action { return nil },
		func(string) []update.Action { return nil },
		func() []update.Action { return nil },
	))
	node := &layout.LayoutNode{ID: 12, BorderRect: geom.Rect{W: 48, H: 1}}
	buf := paint.NewBuffer(geom.Rect{W: 48, H: 1})
	editor.Paint(buf, node, &layout.PaintContext{FocusedID: 12})

	if got, want := strings.TrimRight(buf.String(), " "), strings.TrimRight(newRowParts("name", "ac_", "save ↵", true).render(48, DefaultLineLayoutPolicy()), " "); got != want {
		t.Fatalf("paint = %q, want %q", got, want)
	}
}

func TestInlineEditor_EmptyActiveUsesCaretOnly(t *testing.T) {
	t.Parallel()

	editor := build(NewInlineEditor[struct{}](
		"name",
		"",
		"choose a workspace name",
		DefaultLineLayoutPolicy(),
		func(string) []update.Action { return nil },
		func(string) []update.Action { return nil },
		func() []update.Action { return nil },
	))
	node := &layout.LayoutNode{ID: 13, BorderRect: geom.Rect{W: 64, H: 1}}
	buf := paint.NewBuffer(geom.Rect{W: 64, H: 1})
	editor.Paint(buf, node, &layout.PaintContext{FocusedID: 13})

	got := buf.String()
	if strings.Contains(got, "choose a workspace name_") {
		t.Fatalf("focused empty editor must hide placeholder, got %q", got)
	}
	if want := strings.TrimRight(newRowParts("name", "_", "save ↵", true).render(64, DefaultLineLayoutPolicy()), " "); strings.TrimRight(got, " ") != want {
		t.Fatalf("paint = %q, want %q", got, want)
	}
}

func TestInlineEditor_BackspaceAtEmpty_IsStableAndKeepsEmpty(t *testing.T) {
	t.Parallel()

	var changed []string
	editor := build(NewInlineEditor[struct{}](
		"env",
		"",
		"AWS_BEARER_TOKEN_BEDROCK",
		DefaultLineLayoutPolicy(),
		func(value string) []update.Action {
			changed = append(changed, value)
			return nil
		},
		func(string) []update.Action { return nil },
		func() []update.Action { return nil },
	))
	h := editor.(interaction.EventHandler)

	for i := 0; i < 5; i++ {
		_ = h.HandleEvent(interactionBackspace(), nil)
	}

	if len(changed) == 0 {
		t.Fatalf("expected change callbacks on backspace-at-empty")
	}
	if got := changed[len(changed)-1]; got != "" {
		t.Fatalf("final value=%q want empty", got)
	}
}

func TestInlineEditor_EraseToEmptyThenCommit_SavesEmptyValue(t *testing.T) {
	t.Parallel()

	var committed string
	editor := build(NewInlineEditor[struct{}](
		"env",
		"OPENAI_API_KEY",
		"env variable",
		DefaultLineLayoutPolicy(),
		func(string) []update.Action { return nil },
		func(value string) []update.Action {
			committed = value
			return nil
		},
		func() []update.Action { return nil },
	))
	h := editor.(interaction.EventHandler)

	for i := 0; i < len("OPENAI_API_KEY")+3; i++ {
		_ = h.HandleEvent(interactionBackspace(), nil)
	}
	_ = h.HandleEvent(interactionKey(interaction.KeyEnter), nil)

	if committed != "" {
		t.Fatalf("commit=%q want empty", committed)
	}
}

func TestInlineEditor_TypeThenCancel_UsesCancelPath(t *testing.T) {
	t.Parallel()

	var changed string
	cancelled := false
	editor := build(NewInlineEditor[struct{}](
		"name",
		"acme",
		"workspace",
		DefaultLineLayoutPolicy(),
		func(value string) []update.Action {
			changed = value
			return nil
		},
		func(string) []update.Action { return nil },
		func() []update.Action {
			cancelled = true
			return nil
		},
	))
	h := editor.(interaction.EventHandler)

	_ = h.HandleEvent(interactionRune('-'), nil)
	_ = h.HandleEvent(interactionRune('2'), nil)
	_ = h.HandleEvent(interactionKey(interaction.KeyEsc), nil)

	if changed != "acme-2" {
		t.Fatalf("changed=%q want acme-2", changed)
	}
	if !cancelled {
		t.Fatalf("cancelled=%v want true", cancelled)
	}
}

func TestAnchoredDisclosure_RendersParentThenDetails(t *testing.T) {
	t.Parallel()

	disclosure := build(NewAnchoredDisclosure[struct{}](
		NewChoiceRow[struct{}]("model", "gpt-4.1", noopActions),
		NewStaticValueRow[struct{}]("", "-> 1 gpt-4.1"),
		NewStaticValueRow[struct{}]("", "-> 2 gpt-4.1-mini"),
	))

	node := &layout.LayoutNode{ID: 11, BorderRect: geom.Rect{W: 40, H: 3}, Slot: geom.Rect{W: 40, H: 3}}
	ctx := &layout.LayoutContext{}
	node.MeasuredSize = disclosure.Measure(geom.Exact(40, 3), ctx)
	arranged := disclosure.Arrange(node, ctx)
	if len(arranged.ChildSlots) != 3 {
		t.Fatalf("child slots = %d, want 3", len(arranged.ChildSlots))
	}
	if arranged.ChildSlots[1].Rect.Y <= arranged.ChildSlots[0].Rect.Y {
		t.Fatalf("detail row Y = %d, want below parent Y %d", arranged.ChildSlots[1].Rect.Y, arranged.ChildSlots[0].Rect.Y)
	}
}

func TestChoiceList_HorizontalSelection(t *testing.T) {
	t.Parallel()

	selected := -1
	list := NewChoiceList([]string{"a", "b"}, 0, ChoiceListAxisHorizontal, nil, func(index int) []update.Action {
		selected = index
		return nil
	})
	h := any(list).(interaction.EventHandler)
	h.HandleEvent(interactionKey(interaction.KeyRight), &layout.LayoutNode{BorderRect: geom.Rect{X: 0}})
	if selected != 1 {
		t.Fatalf("selected = %d, want 1", selected)
	}
}

func TestFormatKeyValueTextLine_UsesFixedKeyColumn(t *testing.T) {
	t.Parallel()

	got := FormatKeyValueTextLine("error_origin", "swobu", 12)
	want := "error_origin swobu"
	if got != want {
		t.Fatalf("FormatKeyValueTextLine()=%q want=%q", got, want)
	}
}

func TestRenderKeyValueTextLine_ClipsToWidth(t *testing.T) {
	t.Parallel()

	got := RenderKeyValueTextLine(12, "error_origin", "swobu", 12)
	if len([]rune(got)) != 12 {
		t.Fatalf("RenderKeyValueTextLine width=%d want=12", len([]rune(got)))
	}
	if !strings.HasPrefix(got, "error_origi") {
		t.Fatalf("RenderKeyValueTextLine=%q want clipped prefix", got)
	}
}

func TestWrapLineRowsPreserveIndent_MapsSegmentsToRows(t *testing.T) {
	t.Parallel()

	line := "  alpha beta gamma"
	segments := WrapLinePreserveIndent(line, 10)
	var mapped []string
	rows := WrapLineRowsPreserveIndent[struct{}](line, 10, func(segment string) retained.ViewSpec[struct{}] {
		mapped = append(mapped, segment)
		return NewStaticValueRow[struct{}]("", segment)
	})

	if len(rows) != len(segments) {
		t.Fatalf("rows len = %d, want %d", len(rows), len(segments))
	}
	if len(mapped) != len(segments) {
		t.Fatalf("mapped len = %d, want %d", len(mapped), len(segments))
	}
	for i := range segments {
		if mapped[i] != segments[i] {
			t.Fatalf("mapped[%d] = %q, want %q", i, mapped[i], segments[i])
		}
	}
}

func TestTrimToWidth_SanitizesControlSequences(t *testing.T) {
	t.Parallel()

	in := "abc\x1b[31mRED\x1b[0m\r\n\txyz"
	got := TrimToWidth(in, 64)
	if strings.ContainsRune(got, '\x1b') {
		t.Fatalf("trimmed output contains escape rune: %q", got)
	}
	if strings.ContainsRune(got, '\r') || strings.ContainsRune(got, '\n') || strings.ContainsRune(got, '\t') {
		t.Fatalf("trimmed output contains control whitespace rune: %q", got)
	}
	if !strings.Contains(got, "abc") || !strings.Contains(got, "RED") || !strings.Contains(got, "xyz") {
		t.Fatalf("trimmed output lost expected content: %q", got)
	}
}

func TestRenderEvidenceRow_SanitizesControlSequences(t *testing.T) {
	t.Parallel()

	line := RenderEvidenceRow(120, EvidenceRowSpec{
		Marker: ">",
		Time:   "12:00:00",
		Kind:   "ok",
		Route:  "r\x1b[2Joute",
		Timing: "1ms",
		Result: "c 0%",
		Action: "open ↵",
	})
	if strings.ContainsRune(line, '\x1b') {
		t.Fatalf("evidence row contains escape rune: %q", line)
	}
}

func build(w retained.ViewSpec[struct{}]) layout.RenderNode { return retained.Materialize(nil, w) }

func noopActions() []update.Action { return nil }

func interactionKey(key interaction.Key) interaction.Event {
	return interaction.Event{Kind: interaction.EventKey, Key: key}
}

func interactionRune(r rune) interaction.Event {
	return interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyRune, Rune: r}
}

func interactionBackspace() interaction.Event {
	return interaction.Event{Kind: interaction.EventKey, Key: interaction.KeyBackspace}
}
