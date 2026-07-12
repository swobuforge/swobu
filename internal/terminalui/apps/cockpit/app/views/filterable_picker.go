package views

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
	"github.com/swobuforge/swobu/internal/terminalui/view/retained"
)

type FilterablePickerItem struct {
	// Key carries stable focus identity when the item has one.
	Key      string
	Label    string
	Search   string
	Selected bool
	OnChoose func() []update.Action
}

type FilterablePickerState struct {
	Query           string
	Cursor          int
	Offset          int
	IgnoreNextEnter bool
}

type FilterablePickerConfig struct {
	KeyPrefix         string
	BuildOptionRow    func(item FilterablePickerItem, onCancel func() []update.Action) retained.ViewSpec[state.Model]
	WindowSize        int
	MinOptionsForFind int
	ShowSelected      bool
	NonFilterable     bool
	FindLabel         string
	NoMatchesLabel    string
	HeaderRows        []retained.ViewSpec[state.Model]
	OnCancel          func() []update.Action
	OnNoMatchFocus    func() []update.Action
}

func DefaultFilterablePickerState() FilterablePickerState {
	return FilterablePickerState{Query: "", Cursor: 0, Offset: 0, IgnoreNextEnter: true}
}

func ResetFilterablePickerState(set func(FilterablePickerState)) {
	set(DefaultFilterablePickerState())
}

func FilterablePickerFocusKey(prefix string, filteredIndex int) string {
	if filteredIndex < 0 {
		filteredIndex = 0
	}
	base := strings.TrimSpace(prefix) // swobu:io-string source=boundary
	if base == "" {
		base = "picker-option"
	}
	return fmt.Sprintf("%s/%d", base, filteredIndex)
}

func RenderFilterablePickerDisclosure(
	ctx *retained.Context[state.Model],
	parent retained.ViewSpec[state.Model],
	currentState FilterablePickerState,
	setState func(FilterablePickerState),
	items []FilterablePickerItem,
	cfg FilterablePickerConfig,
) retained.ViewSpec[state.Model] {
	rows, _, nextState := filterablePickerRows(currentState, items, cfg)
	if nextState != currentState {
		setState(nextState)
	}
	disclosure := toolkitviews.NewAnchoredDisclosure(parent, rows...)
	return toolkitviews.KeyScope(disclosure, func(_ *retained.Context[state.Model], ev interaction.Event) (bool, []update.Action) {
		return handleFilterablePickerEvent(ev, nextState, setState, items, cfg)
	})
}

func handleFilterablePickerEvent(
	ev interaction.Event,
	nextState FilterablePickerState,
	setState func(FilterablePickerState),
	items []FilterablePickerItem,
	cfg FilterablePickerConfig,
) (bool, []update.Action) {
	if ev.Kind != interaction.EventKey {
		return false, nil
	}
	cur := nextState
	filteredNow := filterablePickerItems(items, cur.Query)
	switch ev.Key {
	case interaction.KeyUp:
		return handleFilterableKeyUp(cur, setState, filteredNow, cfg)
	case interaction.KeyDown:
		return handleFilterableKeyDown(cur, setState, filteredNow, cfg)
	case interaction.KeyBackspace:
		return handleFilterableKeyBackspace(cur, setState, items, cfg)
	case interaction.KeyEsc:
		return handleFilterableEsc(cur, setState, items, cfg)
	case interaction.KeyEnter:
		return handleFilterableKeyEnter(cur, setState, filteredNow)
	case interaction.KeySpace:
		return handleFilterableKeyRune(cur, setState, items, cfg, ' ')
	case interaction.KeyRune:
		if ev.Rune < 0x20 || ev.Rune == 0x7f {
			return true, nil
		}
		return handleFilterableKeyRune(cur, setState, items, cfg, ev.Rune)
	default:
		return true, nil
	}
}

func handleFilterableKeyUp(cur FilterablePickerState, setState func(FilterablePickerState), filteredNow []FilterablePickerItem, cfg FilterablePickerConfig) (bool, []update.Action) {
	cur.IgnoreNextEnter = false
	if len(filteredNow) == 0 || cur.Cursor <= 0 {
		if cfg.OnCancel != nil {
			return true, cfg.OnCancel()
		}
		return true, nil
	}
	cur.Cursor--
	if cur.Cursor < cur.Offset {
		cur.Offset = cur.Cursor
	}
	setState(cur)
	return true, []update.Action{interaction.FocusKeyAction{Key: FilterablePickerItemFocusKey(filteredNow, cfg, cur.Cursor)}}
}

func handleFilterableKeyDown(cur FilterablePickerState, setState func(FilterablePickerState), filteredNow []FilterablePickerItem, cfg FilterablePickerConfig) (bool, []update.Action) {
	cur.IgnoreNextEnter = false
	if len(filteredNow) == 0 || cur.Cursor >= len(filteredNow)-1 {
		if cfg.OnCancel != nil {
			return true, cfg.OnCancel()
		}
		return true, nil
	}
	cur.Cursor++
	window := cfg.WindowSize
	if window <= 0 {
		window = ListMaxHeight
	}
	if window <= 0 {
		window = 6
	}
	if cur.Cursor >= cur.Offset+window {
		cur.Offset = cur.Cursor - window + 1
	}
	setState(cur)
	return true, []update.Action{interaction.FocusKeyAction{Key: FilterablePickerItemFocusKey(filteredNow, cfg, cur.Cursor)}}
}

func handleFilterableKeyBackspace(cur FilterablePickerState, setState func(FilterablePickerState), items []FilterablePickerItem, cfg FilterablePickerConfig) (bool, []update.Action) {
	if !filterablePickerFilterable(cfg) {
		return true, nil
	}
	cur.IgnoreNextEnter = false
	cur.Query = trimLastRune(cur.Query)
	cur.Cursor = 0
	cur.Offset = 0
	setState(cur)
	return true, focusActionsAfterQueryChange(items, cfg, cur.Query)
}

func handleFilterableKeyEnter(cur FilterablePickerState, setState func(FilterablePickerState), filteredNow []FilterablePickerItem) (bool, []update.Action) {
	if cur.IgnoreNextEnter {
		cur.IgnoreNextEnter = false
		setState(cur)
		return true, nil
	}
	if len(filteredNow) == 0 {
		return true, nil
	}
	selected := filteredNow[cur.Cursor]
	if selected.OnChoose == nil {
		return true, nil
	}
	return true, selected.OnChoose()
}

func handleFilterableKeyRune(cur FilterablePickerState, setState func(FilterablePickerState), items []FilterablePickerItem, cfg FilterablePickerConfig, input rune) (bool, []update.Action) {
	if !filterablePickerFilterable(cfg) {
		return true, nil
	}
	cur.IgnoreNextEnter = false
	setState(cur)
	return handleFilterableQueryInput(cur, setState, items, cfg, input)
}

func handleFilterableEsc(
	current FilterablePickerState,
	setState func(FilterablePickerState),
	items []FilterablePickerItem,
	cfg FilterablePickerConfig,
) (bool, []update.Action) {
	cur := current
	if strings.TrimSpace(cur.Query) != "" { // swobu:io-string source=boundary
		cur.Query = ""
		cur.Cursor = 0
		cur.Offset = 0
		setState(cur)
		return true, focusActionsAfterQueryChange(items, cfg, cur.Query)
	}
	if cfg.OnCancel != nil {
		return true, cfg.OnCancel()
	}
	return true, nil
}

func filterablePickerRows(
	current FilterablePickerState,
	items []FilterablePickerItem,
	cfg FilterablePickerConfig,
) ([]retained.ViewSpec[state.Model], []FilterablePickerItem, FilterablePickerState) {
	next := current
	filtered := filterablePickerItems(items, next.Query)
	if len(filtered) == 0 {
		next.Cursor = 0
		next.Offset = 0
	} else {
		if next.Cursor < 0 {
			next.Cursor = 0
		}
		if next.Cursor >= len(filtered) {
			next.Cursor = len(filtered) - 1
		}
		window := filterablePickerWindowSize(cfg)
		if next.Cursor < next.Offset {
			next.Offset = next.Cursor
		}
		if next.Cursor >= next.Offset+window {
			next.Offset = next.Cursor - window + 1
		}
	}
	rows := make([]retained.ViewSpec[state.Model], 0, len(cfg.HeaderRows)+ListMaxHeight+4)
	rows = append(rows, cfg.HeaderRows...)
	if filterablePickerShowFindRow(cfg, len(items), next.Query) {
		findLabel := strings.TrimSpace(cfg.FindLabel) // swobu:io-string source=boundary
		if findLabel == "" {
			findLabel = "find"
		}
		rows = append(rows, RowStatic(findLabel, filterableQueryDisplay(next.Query)))
	}
	if len(filtered) == 0 {
		none := strings.TrimSpace(cfg.NoMatchesLabel) // swobu:io-string source=boundary
		if none == "" {
			none = "no matches"
		}
		rows = append(rows, RowStatic("", none))
		return rows, filtered, next
	}
	window := filterablePickerWindowSize(cfg)
	start, end := ListWindowBounds(len(filtered), next.Offset, window)
	if start > 0 {
		rows = append(rows, RowStatic("", fmt.Sprintf("… %d earlier", start)))
	}
	for i := start; i < end; i++ {
		item := filtered[i]
		itemCopy := item
		key := itemCopy.Key
		if strings.TrimSpace(key) == "" { // swobu:io-string source=boundary
			key = FilterablePickerFocusKey(cfg.KeyPrefix, i)
		}
		buildRow := cfg.BuildOptionRow
		if buildRow == nil {
			buildRow = defaultFilterablePickerOptionRow(cfg.ShowSelected)
		}
		rows = append(rows, retained.Named[state.Model](key, buildRow(itemCopy, cfg.OnCancel)))
	}
	if end < len(filtered) {
		rows = append(rows, RowStatic("", fmt.Sprintf("… %d more", len(filtered)-end)))
	}
	return rows, filtered, next
}

func filterablePickerWindowSize(cfg FilterablePickerConfig) int {
	window := cfg.WindowSize
	if window <= 0 {
		window = ListMaxHeight
	}
	if window <= 0 {
		window = 6
	}
	return window
}

func filterablePickerShowFindRow(cfg FilterablePickerConfig, totalItems int, query string) bool {
	if !filterablePickerFilterable(cfg) {
		return false
	}
	if strings.TrimSpace(query) != "" { // swobu:io-string source=boundary
		return true
	}
	minOptions := cfg.MinOptionsForFind
	if minOptions <= 0 {
		minOptions = filterablePickerWindowSize(cfg) + 1
	}
	return totalItems >= minOptions
}

func filterablePickerItems(items []FilterablePickerItem, query string) []FilterablePickerItem {
	query = strings.ToLower(strings.TrimSpace(query)) // swobu:io-string source=boundary
	if query == "" {
		out := make([]FilterablePickerItem, 0, len(items))
		out = append(out, items...)
		return out
	}
	out := make([]FilterablePickerItem, 0, len(items))
	for i := range items {
		candidate := strings.TrimSpace(items[i].Search) // swobu:io-string source=boundary
		if candidate == "" {
			candidate = strings.TrimSpace(items[i].Label) // swobu:io-string source=boundary
		}
		if strings.Contains(strings.ToLower(candidate), query) { // swobu:io-string source=boundary
			out = append(out, items[i])
		}
	}
	return out
}

func handleFilterableQueryInput(
	current FilterablePickerState,
	setState func(FilterablePickerState),
	items []FilterablePickerItem,
	cfg FilterablePickerConfig,
	r rune,
) (bool, []update.Action) {
	cur := current
	cur.Query += string(r)
	cur.Cursor = 0
	cur.Offset = 0
	setState(cur)
	filtered := filterablePickerItems(items, cur.Query)
	if len(filtered) == 1 && filtered[0].OnChoose != nil {
		typed := strings.TrimSpace(cur.Query)           // swobu:io-string source=boundary
		label := strings.TrimSpace(filtered[0].Label)   // swobu:io-string source=boundary
		search := strings.TrimSpace(filtered[0].Search) // swobu:io-string source=boundary
		if search == "" {
			search = label
		}
		if strings.EqualFold(label, typed) || strings.EqualFold(search, typed) {
			return true, filtered[0].OnChoose()
		}
	}
	return true, focusActionsAfterQueryChange(items, cfg, cur.Query)
}

func filterableQueryDisplay(query string) string {
	return query + "_"
}

func filterablePickerFilterable(cfg FilterablePickerConfig) bool {
	return !cfg.NonFilterable
}

func trimLastRune(s string) string {
	if s == "" {
		return ""
	}
	_, size := utf8.DecodeLastRuneInString(s)
	if size <= 0 || size > len(s) {
		return ""
	}
	return s[:len(s)-size]
}
