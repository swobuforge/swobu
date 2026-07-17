package ui

import (
	"fmt"
	"strings"

	tui "github.com/grindlemire/go-tui"
)

const searchPickerDefaultVisibleRows = 7

// searchPickerQueryRowKey is the ChoiceItem key for the open-set picker's
// virtual "use the typed query" row. It is never a real listed option.
const searchPickerQueryRowKey = "__query__"

// SearchPickerMode is the only semantic prop a SearchPicker needs. A closed
// set picks from listed options. An open set additionally treats the typed
// query itself as a selectable value.
type SearchPickerMode int

const (
	// SearchPickerClosed is the default: the operator must pick a listed
	// option. A non-matching query yields "no matches".
	SearchPickerClosed SearchPickerMode = iota
	// SearchPickerOpen lets the operator also use the typed query as the
	// value: the query renders as a virtual first row with action "use ↵".
	SearchPickerOpen
)

// SelectionSource tells the parent where a SearchPicker value came from.
type SelectionSource int

const (
	// SelectionListed means the operator picked a backing SearchOption.
	SelectionListed SelectionSource = iota
	// SelectionQuery means the operator used the typed query as the value.
	SelectionQuery
)

// Selection is the only thing a SearchPicker returns. The picker does not know
// whether Value is a model id, a path, an env var, a header, or a profile —
// the parent owns that meaning.
type Selection struct {
	Value  string
	Source SelectionSource
}

// SearchOption is one choosable entry in a SearchPicker.
type SearchOption struct {
	ID       string
	Label    string
	Value    string
	Keywords []string
}

// valueOrID returns the canonical value, falling back to ID when unset so
// closed-set callers need not set Value explicitly.
func (o SearchOption) valueOrID() string {
	if o.Value != "" {
		return o.Value
	}
	return o.ID
}

// SearchPicker is a searchable selectable-list scope.
//
// The picker owns query text and option filtering only. It is generic: the
// Mode prop decides whether the typed query is itself a candidate (open set)
// or not (closed set), and the parent decides what any returned Value means.
// Options render as SelectableRow descendants so Cockpit keeps one operator
// selection cursor.
type SearchPicker struct {
	ID        string
	Title     string
	Query     *tui.State[string]
	Options   []SearchOption
	Mode      SearchPickerMode
	AutoFocus bool
	OnSelect  func(Selection)
	OnCancel  func()
	list      *ChoiceList
}

// NewSearchPicker creates a SearchPicker with the given title and options. The
// picker defaults to closed-set; set Mode to SearchPickerOpen for pickers that
// also accept a free-form query value.
func NewSearchPicker(id, title string, options []SearchOption, onSelect func(Selection), onCancel func()) *SearchPicker {
	p := &SearchPicker{
		ID:       id,
		Title:    title,
		Query:    tui.NewState(""),
		Options:  append([]SearchOption(nil), options...),
		OnSelect: onSelect,
		OnCancel: onCancel,
	}
	p.list = NewChoiceList(p.Query)
	p.configureList()
	return p
}

// UpdateProps refreshes configurable fields from a fresh mount while
// preserving local query state.
func (p *SearchPicker) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*SearchPicker)
	if !ok {
		return
	}
	p.ID = f.ID
	p.Title = f.Title
	p.Options = append([]SearchOption(nil), f.Options...)
	p.Mode = f.Mode
	p.AutoFocus = f.AutoFocus
	p.OnSelect = f.OnSelect
	p.OnCancel = f.OnCancel
	p.configureList()
}

// BindApp wires component state to the app lifecycle.
func (p *SearchPicker) BindApp(app *tui.App) {
	p.configureList()
	p.list.BindApp(app)
}

// UnbindApp releases the app handle.
func (p *SearchPicker) UnbindApp() {}

func (p *SearchPicker) filteredOptions() []SearchOption {
	filtered := make([]SearchOption, 0, len(p.Options))
	for _, item := range p.choiceItems() {
		if item.Virtual {
			continue
		}
		if choiceItemMatches(item, p.Query.Get()) {
			filtered = append(filtered, SearchOption{ID: item.Key, Label: item.Label, Keywords: item.Keywords})
		}
	}
	return filtered
}

func searchPickerQueryValue(query string) string {
	return query + "_"
}

func searchPickerOptionKey(opt SearchOption, index int) string {
	if opt.ID != "" {
		return opt.ID
	}
	return fmt.Sprintf("option-%d", index)
}

func (p *SearchPicker) onEscape(ke tui.KeyEvent) {
	if p.OnCancel != nil {
		p.OnCancel()
	}
}

// KeyMap lets the mounted picker scope own Escape even when the framework focus
// and visible option marker temporarily diverge.
func (p *SearchPicker) KeyMap() tui.KeyMap {
	return tui.KeyMap{
		tui.OnPreemptStop(tui.KeyEscape, p.onEscape),
	}
}

func (p *SearchPicker) configureList() {
	if p.list == nil || p.list.Query != p.Query {
		p.list = NewChoiceList(p.Query)
	}
	p.list.VisibleRows = searchPickerDefaultVisibleRows
	p.list.CountFullSet = true
	p.list.QueryItem = p.openSetQueryItem
	p.list.AutoFocus = p.AutoFocus
	p.list.EmptyLabel = "(no matches)"
	p.list.OnEscape = p.onEscape
	p.list.SetItems(p.choiceItems())
}

// choiceItems builds the listed-option rows only. The open-set query row is
// injected at projection time (see openSetQueryItem) so it stays live with the
// query.
func (p *SearchPicker) choiceItems() []ChoiceItem {
	items := make([]ChoiceItem, 0, len(p.Options))
	for i, opt := range p.Options {
		option := opt
		items = append(items, ChoiceItem{
			Key:      searchPickerOptionKey(option, i),
			Label:    option.Label,
			Value:    option.valueOrID(),
			Action:   "select ↵",
			Keywords: option.Keywords,
			Choose: func() {
				if p.OnSelect != nil {
					p.OnSelect(Selection{Value: option.valueOrID(), Source: SelectionListed})
				}
			},
		})
	}
	return items
}

// openSetQueryItem returns the virtual "use the typed query" row for an
// open-set picker, or a zero ChoiceItem (empty Key) when it should not render.
//
// It renders only when Mode is Open, the query is non-empty, and the query is
// not already an exact (case-insensitive) match for a listed option's value —
// in that last case the listed option itself is the candidate, so duplicating
// it as a query row would be noise.
func (p *SearchPicker) openSetQueryItem(query string) ChoiceItem {
	if p.Mode != SearchPickerOpen {
		return ChoiceItem{}
	}
	value := strings.TrimSpace(query)
	if value == "" {
		return ChoiceItem{}
	}
	if p.queryEqualsListedValue(value) {
		return ChoiceItem{}
	}
	candidate := value
	return ChoiceItem{
		Key:           searchPickerQueryRowKey,
		Label:         candidate,
		Value:         candidate,
		Action:        "use ↵",
		Virtual:       true,
		AlwaysVisible: true,
		Choose: func() {
			if p.OnSelect != nil {
				p.OnSelect(Selection{Value: candidate, Source: SelectionQuery})
			}
		},
	}
}

func (p *SearchPicker) queryEqualsListedValue(query string) bool {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return false
	}
	for _, opt := range p.Options {
		if strings.ToLower(opt.valueOrID()) == needle {
			return true
		}
	}
	return false
}

func (p *SearchPicker) choiceList() *ChoiceList {
	if p.list == nil {
		p.configureList()
	}
	return p.list
}

var (
	_ tui.Component    = (*SearchPicker)(nil)
	_ tui.KeyListener  = (*SearchPicker)(nil)
	_ tui.PropsUpdater = (*SearchPicker)(nil)
	_ tui.AppBinder    = (*SearchPicker)(nil)
)
