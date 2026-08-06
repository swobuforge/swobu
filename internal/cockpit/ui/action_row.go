package ui

import tui "github.com/grindlemire/go-tui"

// ActionRowActionWidth is the fixed width for the action hint container.
const ActionRowActionWidth = 14

// ActionRowValueWidth is kept for EditRow text-input widths only.
const ActionRowValueWidth = 35

// ActionRow builds a view-mode cockpit row.
//
// Layout: arrow(2) + label(18) + value(flex-grow:1) + action(14)
//
// The value flexes to push the action container to the right edge of the
// row. The action container has a fixed width. The text inside is
// left-aligned (default), so short hints like "add ↵" start at the left
// edge of the container.
func ActionRow(arrow, label, value, action string, opts ...tui.Option) *tui.Element {
	row := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100),
	)

	arrowText := arrow
	if len(arrowText) < 2 {
		arrowText = arrowText + "  "
	}
	arrowText = arrowText[:2]
	row.AddChild(tui.New(tui.WithText(arrowText), tui.WithWidth(2), tui.WithFlexShrink(0)))

	if label != "" {
		row.AddChild(tui.New(tui.WithText(label), tui.WithWidth(18), tui.WithFlexShrink(0)))
	}

	valueText := value
	if valueText == "" {
		valueText = " "
	}
	// The value is the single flex-grow child that absorbs all width variation.
	// Two constraints handle the OVERFLOW case (a value longer than the column
	// can hold, e.g. a 60-char URL at a narrow width):
	//   - FlexShrink(0) on the arrow/label/action children (above/below) so 100%
	//     of any width deficit falls on the value. go-tui distributes shrink
	//     uniformly across shrinkable children rather than weighting by base
	//     size, so the default shrink=1 on every child would split the deficit
	//     and starve the value of the space it needs to give up.
	//   - MinWidth(0) on the value zeroes go-tui's default min-width floor
	//     (Auto = intrinsic/text width), which would otherwise clamp the value
	//     back up to its full text length and defeat the shrink.
	// Both are no-ops when the value fits. The exact-fit/adjacency case (value
	// fills the column to its last cell) is handled by the separator below.
	row.AddChild(tui.New(
		tui.WithText(valueText),
		tui.WithFlexGrow(1),
		tui.WithWrap(false),
		tui.WithTruncate(true),
		tui.WithMinWidth(0),
	))

	// A reserved separator between the value and the action column. go-tui lays
	// flex children out adjacent, so when the value text fills its column to the
	// last cell the action hint sits flush against it ("…api.aws/v1edit ↵"):
	// there is no width deficit for flex to distribute, so truncation never
	// engages and the overflow constraints above do not help. A fixed gap that
	// does not shrink guarantees a clean break before the action regardless of
	// the value's length. It costs two cells of value space, which only matters
	// at the narrowest widths where the value truncates sooner.
	if action != "" {
		row.AddChild(tui.New(tui.WithWidth(2), tui.WithFlexShrink(0)))

		row.AddChild(tui.New(
			tui.WithText(action),
			tui.WithWidth(ActionRowActionWidth),
			tui.WithFlexShrink(0),
		))
	}

	for _, o := range opts {
		o(row)
	}
	return row
}

// EditRow builds an edit-mode cockpit row with the same layout.
func EditRow(arrow, label string, input *tui.Element, action string, opts ...tui.Option) *tui.Element {
	row := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100),
	)

	arrowText := arrow
	if len(arrowText) < 2 {
		arrowText = arrowText + "  "
	}
	arrowText = arrowText[:2]
	row.AddChild(tui.New(tui.WithText(arrowText), tui.WithWidth(2)))

	if label != "" {
		row.AddChild(tui.New(tui.WithText(label), tui.WithWidth(18)))
	}

	row.AddChild(input)
	row.AddChild(tui.New(tui.WithFlexGrow(1)))

	if action != "" {
		row.AddChild(tui.New(
			tui.WithText(action),
			tui.WithWidth(ActionRowActionWidth),
		))
	}

	for _, o := range opts {
		o(row)
	}
	return row
}
