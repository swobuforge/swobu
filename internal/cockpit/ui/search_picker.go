package ui

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	tui "github.com/grindlemire/go-tui"
)

// SearchOption is one choosable entry in a SearchPicker.
type SearchOption struct {
	ID       string
	Label    string
	Keywords []string
}

// SearchPicker is a bounded, searchable list component.
//
// It is one mounted component with internal query and cursor state.
// Options are filtered by case-insensitive prefix, substring, token-subsequence,
// and keyword match. The picker renders only a bounded visible window with
// a "N of M shown" footer and keyboard navigation (↑↓ PgUp PgDn Enter Esc).
//
// Escape is handled by the containing FocusableControl, which calls the
// picker's OnCancel. SearchPicker does not claim Escape directly.
//
// SearchPicker does not own option semantics; callers provide the option slice
// and OnSelect/OnCancel callbacks.
type SearchPicker struct {
	SelectBase
	Title      string
	Query      *tui.State[string]
	Cursor     *tui.State[int]
	Offset     *tui.State[int]
	Options    []SearchOption
	MaxVisible int
	// AutoFocus seeds the picker as selected on mount, or on the first
	// transition from false to true on an already-mounted picker.
	AutoFocus  bool
	OnSelect   func(SearchOption)
	OnCancel   func()
}

const (
	searchPickerDefaultMaxVisible = 7
	searchPickerQueryWidth        = 18

	scorePrefix    = 400
	scoreSubstring = 300
	scoreKeyword   = 200
	scoreToken     = 100
	scoreLengthMax = 1000
)

// NewSearchPicker creates a SearchPicker with the given title and options.
func NewSearchPicker(id, title string, options []SearchOption, onSelect func(SearchOption), onCancel func()) *SearchPicker {
	return &SearchPicker{
		SelectBase: NewSelectBase(id),
		Title:      title,
		Query:      tui.NewState(""),
		Cursor:     tui.NewState(0),
		Offset:     tui.NewState(0),
		Options:    append([]SearchOption(nil), options...),
		MaxVisible: searchPickerDefaultMaxVisible,
		OnSelect:   onSelect,
		OnCancel:   onCancel,
	}
}

// UpdateProps refreshes configurable fields from a fresh mount while
// preserving local query/cursor state.
func (p *SearchPicker) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*SearchPicker)
	if !ok {
		return
	}
	prevAutoFocus := p.AutoFocus
	p.Title = f.Title
	p.Options = append([]SearchOption(nil), f.Options...)
	p.MaxVisible = f.MaxVisible
	p.AutoFocus = f.AutoFocus
	p.OnSelect = f.OnSelect
	p.OnCancel = f.OnCancel

	if !prevAutoFocus && p.AutoFocus && !p.IsFocused() {
		p.Focus(p.app)
	}
}

// Init seeds the visible focus marker when the picker is configured to autofocus.
func (p *SearchPicker) Init() func() {
	if !p.AutoFocus || p.IsFocused() {
		return nil
	}

	p.focused.Set(true)
	if p.app != nil {
		p.Focus(p.app)
	}
	return nil
}

// BindApp wires component state to the app lifecycle.
func (p *SearchPicker) BindApp(app *tui.App) {
	p.SelectBase.BindApp(app)
	if p.Query != nil {
		p.Query.BindApp(app)
	}
	if p.Cursor != nil {
		p.Cursor.BindApp(app)
	}
	if p.Offset != nil {
		p.Offset.BindApp(app)
	}
}

// UnbindApp releases the app handle.
func (p *SearchPicker) UnbindApp() {
	p.SelectBase.UnbindApp()
}

// KeyMap returns the keyboard bindings for query editing, cursor movement,
// and selection. All bindings are focus-gated on the picker.
//
// Escape is NOT listed here — the parent FocusableControl owns it and calls
// OnCancel from OnExit.
func (p *SearchPicker) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnFocused(tui.AnyRune, p.onType),
		tui.OnFocused(tui.KeyBackspace, p.onBackspace),
		tui.OnFocused(tui.KeyEnter, p.onEnter),
		tui.OnFocused(tui.KeyUp, p.onUp),
		tui.OnFocused(tui.KeyDown, p.onDown),
		tui.OnFocused(tui.KeyPageUp, p.onPageUp),
		tui.OnFocused(tui.KeyPageDown, p.onPageDown),
	}
}

// Render builds the picker element tree: title, query, visible options, footer.
func (p *SearchPicker) Render(app *tui.App) *tui.Element {
	root := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Column),
		tui.WithWidthPercent(100),
		tui.WithFocusable(true),
		tui.WithAutoFocus(p.AutoFocus),
		tui.WithOnFocus(p.OnFocus),
		tui.WithOnBlur(p.OnBlur),
	)

	if p.Title != "" {
		root.AddChild(p.renderTitle())
	}
	root.AddChild(p.renderQuery())

	filtered := p.filteredOptions()
	p.clampCursor(filtered)

	visible, offset := p.computeWindow(filtered)
	p.Offset.Set(offset)

	for i, opt := range visible {
		highlighted := offset+i == p.Cursor.Get()
		root.AddChild(p.renderOption(opt, highlighted))
	}

	root.AddChild(p.renderFooter(len(visible), len(p.Options)))

	if p.Ref != nil {
		p.Ref.Set(root)
	}
	return root
}

// ---------------------------------------------------------------------------
// Rendering helpers
// ---------------------------------------------------------------------------

func (p *SearchPicker) renderTitle() *tui.Element {
	row := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100),
	)
	row.AddChild(tui.New(tui.WithWidth(2)))
	row.AddChild(tui.New(tui.WithText(p.Title)))
	return row
}

func (p *SearchPicker) renderQuery() *tui.Element {
	row := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100),
	)
	row.AddChild(tui.New(tui.WithWidth(2)))
	row.AddChild(tui.New(tui.WithText("search"), tui.WithWidth(searchPickerQueryWidth)))
	row.AddChild(tui.New(tui.WithText(p.Query.Get())))
	if p.IsFocused() {
		row.AddChild(tui.New(tui.WithText("_")))
	}
	return row
}

func (p *SearchPicker) renderOption(opt SearchOption, highlighted bool) *tui.Element {
	row := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100),
	)

	arrow := SelectArrowBlurred
	if highlighted {
		arrow = SelectArrowFocused
	}
	row.AddChild(tui.New(tui.WithText(arrow), tui.WithWidth(2)))
	row.AddChild(tui.New(
		tui.WithText(opt.Label),
		tui.WithTruncate(true),
		tui.WithWrap(false),
	))
	// Keep picker actions within a fixed single-line budget like other cockpit
	// action rows; a flex spacer would overrun the footer/action text.
	row.AddChild(tui.New(tui.WithWidth(ActionRowValueWidth)))

	action := ""
	if highlighted {
		action = "select ↵"
	}
	row.AddChild(tui.New(tui.WithText(action)))
	return row
}

func (p *SearchPicker) renderFooter(shown, total int) *tui.Element {
	row := tui.New(
		tui.WithDisplay(tui.DisplayFlex), tui.WithDirection(tui.Row),
		tui.WithWidthPercent(100),
	)
	row.AddChild(tui.New(tui.WithWidth(2)))

	row.AddChild(tui.New(tui.WithText(fmt.Sprintf("%d of %d shown", shown, total))))
	row.AddChild(tui.New(tui.WithWidth(ActionRowValueWidth)))

	hint := "↑↓ search"
	if total > p.effectiveMaxVisible() {
		hint = "↑↓ PgUp PgDn"
	}
	row.AddChild(tui.New(tui.WithText(hint)))
	return row
}

// ---------------------------------------------------------------------------
// Filtering
// ---------------------------------------------------------------------------

func (p *SearchPicker) filteredOptions() []SearchOption {
	q := strings.TrimSpace(strings.ToLower(p.Query.Get()))
	if q == "" {
		return append([]SearchOption(nil), p.Options...)
	}

	type scoredOption struct {
		option SearchOption
		score  int
		index  int
	}

	var scored []scoredOption
	for i, opt := range p.Options {
		score := p.scoreOption(opt, q)
		if score > 0 {
			scored = append(scored, scoredOption{option: opt, score: score, index: i})
		}
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].index < scored[j].index // stable
	})

	result := make([]SearchOption, len(scored))
	for i, s := range scored {
		result[i] = s.option
	}
	return result
}

func (p *SearchPicker) scoreOption(opt SearchOption, q string) int {
	labelLower := strings.ToLower(opt.Label)
	score := 0

	switch {
	case strings.HasPrefix(labelLower, q):
		score = scorePrefix
	case strings.Contains(labelLower, q):
		score = scoreSubstring
	default:
		for _, kw := range opt.Keywords {
			if strings.Contains(strings.ToLower(kw), q) {
				score = scoreKeyword
				break
			}
		}
		if score == 0 && tokenSubsequenceMatch(labelLower, q) {
			score = scoreToken
		}
	}

	if score > 0 {
		// Shorter label wins ties within the same score tier.
		score += scoreLengthMax - len(opt.Label)
	}
	return score
}

func tokenSubsequenceMatch(label, query string) bool {
	labelTokens := tokenize(label)
	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return true
	}
	if len(queryTokens) > len(labelTokens) {
		return false
	}

	qIdx := 0
	for _, t := range labelTokens {
		if strings.HasPrefix(t, queryTokens[qIdx]) {
			qIdx++
			if qIdx >= len(queryTokens) {
				return true
			}
		}
	}
	return false
}

func tokenize(s string) []string {
	var tokens []string
	for _, word := range strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		if word != "" {
			tokens = append(tokens, strings.ToLower(word))
		}
	}
	return tokens
}

// ---------------------------------------------------------------------------
// Cursor and window
// ---------------------------------------------------------------------------

func (p *SearchPicker) clampCursor(filtered []SearchOption) {
	c := p.Cursor.Get()
	if c < 0 {
		c = 0
	}
	if len(filtered) > 0 && c >= len(filtered) {
		c = len(filtered) - 1
	}
	if len(filtered) == 0 {
		c = 0
	}
	p.Cursor.Set(c)
}

func (p *SearchPicker) computeWindow(filtered []SearchOption) (visible []SearchOption, offset int) {
	max := p.effectiveMaxVisible()
	cursor := p.Cursor.Get()

	if len(filtered) <= max {
		return filtered, 0
	}

	offset = p.Offset.Get()
	if cursor < offset {
		offset = cursor
	} else if cursor >= offset+max {
		offset = cursor - max + 1
	}

	end := offset + max
	if end > len(filtered) {
		end = len(filtered)
		offset = end - max
	}
	if offset < 0 {
		offset = 0
	}

	return filtered[offset:end], offset
}

func (p *SearchPicker) effectiveMaxVisible() int {
	if p.MaxVisible > 0 {
		return p.MaxVisible
	}
	return searchPickerDefaultMaxVisible
}

// ---------------------------------------------------------------------------
// Key handlers
// ---------------------------------------------------------------------------

func (p *SearchPicker) onType(ke tui.KeyEvent) {
	if ke.Rune == 0 {
		return
	}
	p.Query.Set(p.Query.Get() + string(ke.Rune))
	p.Cursor.Set(0)
}

func (p *SearchPicker) onBackspace(ke tui.KeyEvent) {
	q := p.Query.Get()
	if len(q) == 0 {
		return
	}
	_, size := utf8.DecodeLastRuneInString(q)
	if size > 0 && len(q) >= size {
		p.Query.Set(q[:len(q)-size])
	}
	p.Cursor.Set(0)
}

func (p *SearchPicker) onEnter(ke tui.KeyEvent) {
	filtered := p.filteredOptions()
	c := p.Cursor.Get()
	if c >= 0 && c < len(filtered) && p.OnSelect != nil {
		p.OnSelect(filtered[c])
	}
}

func (p *SearchPicker) onEscape(ke tui.KeyEvent) {
	if p.OnCancel != nil {
		p.OnCancel()
	}
}

func (p *SearchPicker) onUp(ke tui.KeyEvent) {
	c := p.Cursor.Get()
	if c > 0 {
		p.Cursor.Set(c - 1)
	}
}

func (p *SearchPicker) onDown(ke tui.KeyEvent) {
	c := p.Cursor.Get()
	last := len(p.filteredOptions()) - 1
	if c < last {
		p.Cursor.Set(c + 1)
	}
}

func (p *SearchPicker) onPageUp(ke tui.KeyEvent) {
	c := p.Cursor.Get()
	p.Cursor.Set(maxInt(0, c-p.effectiveMaxVisible()))
}

func (p *SearchPicker) onPageDown(ke tui.KeyEvent) {
	c := p.Cursor.Get()
	last := len(p.filteredOptions()) - 1
	p.Cursor.Set(minInt(last, c+p.effectiveMaxVisible()))
}

// ---------------------------------------------------------------------------
// Int helpers
// ---------------------------------------------------------------------------

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var (
	_ tui.Component    = (*SearchPicker)(nil)
	_ tui.KeyListener  = (*SearchPicker)(nil)
	_ tui.PropsUpdater = (*SearchPicker)(nil)
	_ tui.AppBinder    = (*SearchPicker)(nil)
	_ tui.Initializer  = (*SearchPicker)(nil)
)
