package ui

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/ui/interaction"
)

const choiceListDefaultVisibleRows = 7

// ChoiceItem is one semantic option in a ChoiceList.
type ChoiceItem struct {
	Key           string
	Label         string
	Value         string
	Action        string
	Keywords      []string
	Choose        func()
	AlwaysVisible bool
	// Virtual marks a row that renders and is selectable but is not a real
	// listed option — e.g. an open-set picker's "use the typed query" row.
	// Virtual rows are excluded from the shown/total footer counts.
	Virtual bool
}

// ChoiceRowModel is one rendered row in a ChoiceList projection.
type ChoiceRowModel struct {
	Item  ChoiceItem
	Index int
}

// ChoiceWindow is the render model for a ChoiceList.
type ChoiceWindow struct {
	Rows      []ChoiceRowModel
	TotalRows int
	ShownRows int
}

// ChoiceList owns shared clipped-list interaction state.
//
// Domain wrappers provide items and callbacks. ChoiceList owns filtering,
// visible-row projection, and projection shifts when selection moves across the
// clipped window edge. Query mutation is opt-in for query-bearing presentations;
// closed-choice presentations reuse the same list with QueryEditing disabled.
type ChoiceList struct {
	Query       *tui.State[string]
	WindowStart *tui.State[int]
	FocusKey    *tui.State[string]

	Items       []ChoiceItem
	VisibleRows int
	// ShowAllRows explicitly opts out of the default clipped projection.
	ShowAllRows bool
	// CountFullSet reports the footer denominator against the full backing
	// option set rather than the filtered match set, so the count reads
	// "X of Y shown" where Y is the size of the world being searched.
	CountFullSet bool
	// QueryItem, when set, returns a virtual row derived from the live query
	// (e.g. an open-set picker's "use the typed query" candidate). It is
	// evaluated on every projection so it stays live as the operator types.
	// Returning a ChoiceItem with an empty Key renders nothing.
	QueryItem func(query string) ChoiceItem
	AutoFocus bool
	// QueryEditing enables rune/backspace mutation for query-bearing
	// presentations such as SearchPicker and FileBrowser. Closed-choice
	// presentations leave it false so invisible text input is impossible.
	QueryEditing bool
	EmptyLabel   string
	OnEscape     func(tui.KeyEvent)
}

// NewChoiceList creates a list kernel over an existing query state.
func NewChoiceList(query *tui.State[string]) *ChoiceList {
	if query == nil {
		query = tui.NewState("")
	}
	return &ChoiceList{
		Query:       query,
		WindowStart: tui.NewState(0),
		FocusKey:    tui.NewState(""),
	}
}

// BindApp wires list state to go-tui.
func (l *ChoiceList) BindApp(app *tui.App) {
	l.Query.BindApp(app)
	l.WindowStart.BindApp(app)
	l.FocusKey.BindApp(app)
}

// ResetProjection clears viewport-only projection state.
func (l *ChoiceList) ResetProjection() {
	l.WindowStart.Set(0)
	l.FocusKey.Set("")
}

// SetItems replaces the backing options and repairs viewport state.
func (l *ChoiceList) SetItems(items []ChoiceItem) {
	l.Items = items
	l.RepairProjection()
}

// RepairProjection bounds viewport-only projection state to the current rows.
func (l *ChoiceList) RepairProjection() {
	rows := l.filteredRows()
	start := boundedChoiceWindowStart(l.WindowStart.Get(), len(rows), l.visibleRows(len(rows)))
	l.WindowStart.Set(start)
}

// RevealFocus finds a row in the complete filtered model, reveals it, and
// publishes its focus key. An unmatched selection falls back to the first row.
func (l *ChoiceList) RevealFocus(match func(ChoiceItem) bool) bool {
	rows := l.filteredRows()
	if len(rows) == 0 {
		l.FocusKey.Set("")
		l.WindowStart.Set(0)
		return false
	}
	selected := 0
	found := false
	for i, row := range rows {
		if match(row.Item) {
			selected, found = i, true
			break
		}
	}
	visible := l.visibleRows(len(rows))
	start := l.WindowStart.Get()
	if selected < start {
		start = selected
	} else if selected >= start+visible {
		start = selected - visible + 1
	}
	l.WindowStart.Set(boundedChoiceWindowStart(start, len(rows), visible))
	l.FocusKey.Set(choiceRowKey(rows[selected]))
	return found
}

// Window returns the current filtered and clipped projection.
func (l *ChoiceList) Window() ChoiceWindow {
	rows := l.filteredRows()
	start := boundedChoiceWindowStart(l.WindowStart.Get(), len(rows), l.visibleRows(len(rows)))
	end := start + l.visibleRows(len(rows))
	if end > len(rows) {
		end = len(rows)
	}
	if start > end {
		start = end
	}
	projected := rows[start:end]
	total := l.countRealRows(rows)
	if l.CountFullSet {
		total = l.countBackingRows()
	}
	return ChoiceWindow{
		Rows:      projected,
		TotalRows: total,
		ShownRows: l.countRealRows(projected),
	}
}

// countBackingRows counts non-virtual backing items, ignoring the query
// filter. It is the full-set denominator for lists that report against the
// size of the searched world rather than the filtered match set.
func (l *ChoiceList) countBackingRows() int {
	count := 0
	for _, item := range l.Items {
		if item.Virtual {
			continue
		}
		count++
	}
	return count
}

// CountLabel formats the standard shown/total footer.
func (l *ChoiceList) CountLabel(win ChoiceWindow) string {
	return fmt.Sprintf("%d of %d shown", win.ShownRows, win.TotalRows)
}

func (l *ChoiceList) filteredRows() []ChoiceRowModel {
	query := strings.ToLower(strings.TrimSpace(l.Query.Get()))
	rows := make([]ChoiceRowModel, 0, len(l.Items)+1)
	if l.QueryItem != nil {
		if qi := l.QueryItem(l.Query.Get()); qi.Key != "" {
			rows = append(rows, ChoiceRowModel{Item: qi, Index: len(rows)})
		}
	}
	for _, item := range l.Items {
		if query != "" && !item.AlwaysVisible && !choiceItemMatches(item, query) {
			continue
		}
		rows = append(rows, ChoiceRowModel{Item: item, Index: len(rows)})
	}
	if len(rows) == 0 && l.EmptyLabel != "" {
		rows = append(rows, ChoiceRowModel{Item: ChoiceItem{Key: "no-match", Label: l.EmptyLabel}, Index: 0})
	}
	return rows
}

func (l *ChoiceList) countRealRows(rows []ChoiceRowModel) int {
	count := 0
	for _, row := range rows {
		if row.Item.Virtual {
			continue
		}
		if row.Item.Key == "no-match" && row.Item.Choose == nil {
			continue
		}
		count++
	}
	return count
}

func choiceItemMatches(item ChoiceItem, query string) bool {
	haystack := normalizeChoiceSearchText(item.Label + " " + item.Value + " " + strings.Join(item.Keywords, " "))
	for _, token := range strings.Fields(normalizeChoiceSearchText(query)) {
		if !strings.Contains(haystack, token) {
			return false
		}
	}
	return true
}

func normalizeChoiceSearchText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' {
			return unicode.ToLower(r)
		}
		return ' '
	}, value)
}

func (l *ChoiceList) visibleRows(total int) int {
	if l.ShowAllRows {
		if total > 0 {
			return total
		}
		return 1
	}
	if l.VisibleRows > 0 {
		return l.VisibleRows
	}
	return choiceListDefaultVisibleRows
}

func boundedChoiceWindowStart(start, total, visible int) int {
	if visible < 1 {
		visible = 1
	}
	maxStart := total - visible
	if maxStart < 0 {
		maxStart = 0
	}
	if start < 0 {
		return 0
	}
	if start > maxStart {
		return maxStart
	}
	return start
}

func (l *ChoiceList) repairSelectionAfterQuery(previousKey string) {
	rows := l.filteredRows()
	if len(rows) == 0 {
		l.ResetProjection()
		return
	}
	selected, foundPrevious := 0, false
	if previousKey != "" {
		for i, row := range rows {
			if choiceRowKey(row) == previousKey {
				selected = i
				foundPrevious = true
				break
			}
		}
	}
	if !foundPrevious {
		for i, row := range rows {
			if row.Item.Key == "no-match" && row.Item.Choose == nil {
				continue
			}
			selected = i
			break
		}
	}
	visible := l.visibleRows(len(rows))
	start := boundedChoiceWindowStart(l.WindowStart.Get(), len(rows), visible)
	if selected < start {
		start = selected
	}
	if selected >= start+visible {
		start = selected - visible + 1
	}
	l.WindowStart.Set(boundedChoiceWindowStart(start, len(rows), visible))
	l.FocusKey.Set(choiceRowKey(rows[selected]))
}

func (l *ChoiceList) onTypeFromRow(row ChoiceRowModel, ke tui.KeyEvent) {
	if ke.Rune == 0 {
		return
	}
	l.Query.Set(l.Query.Get() + string(ke.Rune))
	l.repairSelectionAfterQuery(choiceRowKey(row))
}

func (l *ChoiceList) onBackspaceFromRow(row ChoiceRowModel, _ tui.KeyEvent) {
	q := l.Query.Get()
	if len(q) == 0 {
		return
	}
	_, size := utf8.DecodeLastRuneInString(q)
	if size > 0 && len(q) >= size {
		l.Query.Set(q[:len(q)-size])
		l.repairSelectionAfterQuery(choiceRowKey(row))
	}
}

func (l *ChoiceList) moveProjectionFrom(row ChoiceRowModel, delta int) bool {
	rows := l.filteredRows()
	next := row.Index + delta
	if next < 0 || next >= len(rows) {
		return false
	}
	start := boundedChoiceWindowStart(l.WindowStart.Get(), len(rows), l.visibleRows(len(rows)))
	nextStart := start
	visible := l.visibleRows(len(rows))
	if next < start {
		nextStart = next
	}
	if next >= start+visible {
		nextStart = next - visible + 1
	}
	if nextStart == start {
		return false
	}
	l.WindowStart.Set(nextStart)
	l.FocusKey.Set(choiceRowKey(rows[next]))
	return true
}

func (l *ChoiceList) pageProjectionFrom(row ChoiceRowModel, direction int) bool {
	rows := l.filteredRows()
	if len(rows) == 0 || direction == 0 {
		return false
	}
	visible := l.visibleRows(len(rows))
	delta := visible
	if direction < 0 {
		delta = -visible
	}
	next := row.Index + delta
	if next < 0 {
		next = 0
	}
	if next >= len(rows) {
		next = len(rows) - 1
	}
	if next == row.Index {
		return false
	}
	l.WindowStart.Set(boundedChoiceWindowStart(next, len(rows), visible))
	l.FocusKey.Set(choiceRowKey(rows[next]))
	return true
}

func choiceRowKey(row ChoiceRowModel) string {
	if row.Item.Key != "" {
		return row.Item.Key
	}
	if row.Item.Label != "" {
		return row.Item.Label
	}
	return fmt.Sprintf("row-%d", row.Index)
}

// ChoiceRow is the mounted selectable component for one ChoiceList option.
type ChoiceRow struct {
	target *interaction.Selectable

	List      *ChoiceList
	Row       ChoiceRowModel
	AutoFocus bool
}

// NewChoiceRow creates a mounted row for a ChoiceList projection.
func NewChoiceRow(id string, list *ChoiceList, row ChoiceRowModel, autofocus bool) *ChoiceRow {
	r := &ChoiceRow{List: list, Row: row, AutoFocus: autofocus}
	r.target = interaction.NewSelectable(r.propsWithID(id))
	return r
}

// BindApp wires row focus state.
func (r *ChoiceRow) BindApp(app *tui.App) { r.target.BindApp(app) }

// UnbindApp releases row focus state.
func (r *ChoiceRow) UnbindApp() { r.target.UnbindApp() }

// UpdateProps updates the mounted row while preserving focus state.
func (r *ChoiceRow) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*ChoiceRow)
	if !ok {
		return
	}
	r.List = f.List
	r.Row = f.Row
	r.AutoFocus = f.AutoFocus
	r.target.Update(r.props())
}

// Init seeds autofocus for the first mounted row or projection-repair row.
func (r *ChoiceRow) Init() func() {
	r.target.Update(r.props())
	return r.target.Init()
}

// IsFocused satisfies go-tui focused dispatch.
func (r *ChoiceRow) IsFocused() bool { return r.target.IsFocused() }

// Render returns the standard choice row layout.
func (r *ChoiceRow) Render(*tui.App) *tui.Element {
	r.target.SetRenderProps(r.props())
	opts := append(r.target.ShellOptions(), tui.WithOnActivate(r.choose))
	root := ActionRow(r.target.Marker(), "", r.Row.Item.Label, r.Row.Item.Action, opts...)
	r.target.BindElement(root)
	return root
}

// KeyMap owns choice typing, Escape, projection-aware movement, and activation.
func (r *ChoiceRow) KeyMap() tui.KeyMap {
	keys := ActivateSelected(func(tui.KeyEvent) { r.choose() })
	if r.List != nil {
		if r.List.QueryEditing {
			keys = append(keys,
				tui.OnFocused(tui.AnyRune, func(ke tui.KeyEvent) { r.List.onTypeFromRow(r.Row, ke) }),
				tui.OnFocused(tui.KeyBackspace, func(ke tui.KeyEvent) { r.List.onBackspaceFromRow(r.Row, ke) }),
			)
		}
		if r.List.OnEscape != nil {
			keys = append(keys, tui.OnFocused(tui.KeyEscape, r.List.OnEscape))
		}
	}
	keys = append(keys,
		tui.OnFocused(tui.KeyDown, func(ke tui.KeyEvent) {
			if r.List != nil && r.List.moveProjectionFrom(r.Row, 1) {
				return
			}
			SelectNext(ke)
		}),
		tui.OnFocused(tui.KeyUp, func(ke tui.KeyEvent) {
			if r.List != nil && r.List.moveProjectionFrom(r.Row, -1) {
				return
			}
			SelectPrevious(ke)
		}),
		tui.OnFocused(tui.KeyPageDown, func(tui.KeyEvent) {
			if r.List != nil {
				r.List.pageProjectionFrom(r.Row, 1)
			}
		}),
		tui.OnFocused(tui.KeyPageUp, func(tui.KeyEvent) {
			if r.List != nil {
				r.List.pageProjectionFrom(r.Row, -1)
			}
		}),
	)
	return keys
}

func (r *ChoiceRow) choose() {
	if r.Row.Item.Choose != nil {
		r.Row.Item.Choose()
	}
}

func (r *ChoiceRow) props() interaction.SelectableProps {
	return r.propsWithID(r.target.Props().ID)
}

func (r *ChoiceRow) propsWithID(id string) interaction.SelectableProps {
	return interaction.SelectableProps{
		ID:        id,
		Label:     r.Row.Item.Label,
		Action:    r.Row.Item.Action,
		AutoFocus: r.AutoFocus || (r.List != nil && r.List.FocusKey.Get() == choiceRowKey(r.Row)),
		OnActivate: func(interaction.Context) {
			r.choose()
		},
	}
}

var (
	_ tui.Component    = (*ChoiceRow)(nil)
	_ tui.KeyListener  = (*ChoiceRow)(nil)
	_ tui.AppBinder    = (*ChoiceRow)(nil)
	_ tui.Initializer  = (*ChoiceRow)(nil)
	_ tui.PropsUpdater = (*ChoiceRow)(nil)
)
