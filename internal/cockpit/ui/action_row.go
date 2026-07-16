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
	row.AddChild(tui.New(tui.WithText(arrowText), tui.WithWidth(2)))

	if label != "" {
		row.AddChild(tui.New(tui.WithText(label), tui.WithWidth(18)))
	}

	valueText := value
	if valueText == "" {
		valueText = " "
	}
	row.AddChild(tui.New(
		tui.WithText(valueText),
		tui.WithFlexGrow(1),
		tui.WithWrap(false),
		tui.WithTruncate(true),
	))

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
