package views

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/interaction"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/update"
	"github.com/swobuforge/swobu/internal/terminalui/engine/retained/view"
	toolkitviews "github.com/swobuforge/swobu/internal/terminalui/toolkit/views"
)

type FilterablePickerItem struct {
	Key      string
	Label    string
	Search   string
	Selected bool
	OnChoose func() []update.Action
}

type FilterablePickerState struct {
	Query  string
	Cursor int
	Offset int
}

type FilterablePickerConfig struct {
	KeyPrefix         string
	BuildOptionRow    func(item FilterablePickerItem, onCancel func() []update.Action) view.ViewSpec[state.Model]
	WindowSize        int
	MinOptionsForFind int
	ShowSelected      bool
	FindLabel         string
	NoMatchesLabel    string
	HeaderRows        []view.ViewSpec[state.Model]
	OnCancel          func() []update.Action
	OnNoMatchFocus    func() []update.Action
}

func DefaultFilterablePickerState() FilterablePickerState {
	return FilterablePickerState{Query: "", Cursor: 0, Offset: 0}
}

func ResetFilterablePickerState(set func(FilterablePickerState)) {
	set(DefaultFilterablePickerState())
}

func FilterablePickerFocusKey(prefix string, filteredIndex int) string {
	if filteredIndex < 0 {
		filteredIndex = 0
	}
	base := strings.TrimSpace(prefix)
	if base == "" {
		base = "picker-option"
	}
	return fmt.Sprintf("%s/%d", base, filteredIndex)
}

func RenderFilterablePickerDisclosure(
	ctx *view.Context[state.Model],
	parent view.ViewSpec[state.Model],
	currentState FilterablePickerState,
	setState func(FilterablePickerState),
	items []FilterablePickerItem,
	cfg FilterablePickerConfig,
) view.ViewSpec[state.Model] {
	rows, _, nextState := filterablePickerRows(currentState, items, cfg)
	if nextState != currentState {
		setState(nextState)
	}
	disclosure := toolkitviews.NewAnchoredDisclosure(parent, rows...)
	return toolkitviews.KeyScope(disclosure, func(_ *view.Context[state.Model], ev interaction.Event) (bool, []update.Action) {
		if ev.Kind != interaction.EventKey {
			return false, nil
		}
		cur := nextState
		filteredNow := filterablePickerItems(items, cur.Query)
		switch ev.Key {
		case interaction.KeyUp:
			if len(filteredNow) == 0 || cur.Cursor <= 0 {
				return true, nil
			}
			cur.Cursor--
			if cur.Cursor < cur.Offset {
				cur.Offset = cur.Cursor
			}
			setState(cur)
			return true, []update.Action{interaction.FocusKeyAction{Key: filterablePickerItemFocusKey(filteredNow, cfg, cur.Cursor)}}
		case interaction.KeyDown:
			if len(filteredNow) == 0 || cur.Cursor >= len(filteredNow)-1 {
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
			return true, []update.Action{interaction.FocusKeyAction{Key: filterablePickerItemFocusKey(filteredNow, cfg, cur.Cursor)}}
		case interaction.KeyBackspace:
			cur.Query = trimLastRune(cur.Query)
			cur.Cursor = 0
			cur.Offset = 0
			setState(cur)
			return true, focusActionsAfterQueryChange(items, cfg, cur.Query)
		case interaction.KeyEsc:
			if strings.TrimSpace(cur.Query) != "" {
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
		case interaction.KeyEnter:
			if len(filteredNow) == 0 {
				return true, nil
			}
			selected := filteredNow[cur.Cursor]
			if selected.OnChoose == nil {
				return true, nil
			}
			return true, selected.OnChoose()
		case interaction.KeySpace:
			return handleFilterableQueryInput(nextState, setState, items, cfg, ' ')
		case interaction.KeyRune:
			if ev.Rune < 0x20 || ev.Rune == 0x7f {
				return true, nil
			}
			return handleFilterableQueryInput(nextState, setState, items, cfg, ev.Rune)
		default:
			return false, nil
		}
	})
}

func filterablePickerRows(
	current FilterablePickerState,
	items []FilterablePickerItem,
	cfg FilterablePickerConfig,
) ([]view.ViewSpec[state.Model], []FilterablePickerItem, FilterablePickerState) {
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
	rows := make([]view.ViewSpec[state.Model], 0, len(cfg.HeaderRows)+ListMaxHeight+4)
	rows = append(rows, cfg.HeaderRows...)
	if filterablePickerShowFindRow(cfg, len(items), next.Query) {
		findLabel := strings.TrimSpace(cfg.FindLabel)
		if findLabel == "" {
			findLabel = "find"
		}
		rows = append(rows, RowStatic(findLabel, filterableQueryDisplay(next.Query)))
	}
	if len(filtered) == 0 {
		none := strings.TrimSpace(cfg.NoMatchesLabel)
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
		if strings.TrimSpace(key) == "" {
			key = FilterablePickerFocusKey(cfg.KeyPrefix, i)
		}
		buildRow := cfg.BuildOptionRow
		if buildRow == nil {
			buildRow = defaultFilterablePickerOptionRow(cfg.ShowSelected)
		}
		rows = append(rows, view.Named[state.Model](key, buildRow(itemCopy, cfg.OnCancel)))
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
	if strings.TrimSpace(query) != "" {
		return true
	}
	minOptions := cfg.MinOptionsForFind
	if minOptions <= 0 {
		minOptions = filterablePickerWindowSize(cfg) + 1
	}
	return totalItems >= minOptions
}

func filterablePickerItems(items []FilterablePickerItem, query string) []FilterablePickerItem {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		out := make([]FilterablePickerItem, 0, len(items))
		out = append(out, items...)
		return out
	}
	out := make([]FilterablePickerItem, 0, len(items))
	for i := range items {
		candidate := strings.TrimSpace(items[i].Search)
		if candidate == "" {
			candidate = strings.TrimSpace(items[i].Label)
		}
		if strings.Contains(strings.ToLower(candidate), query) {
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
	return true, focusActionsAfterQueryChange(items, cfg, cur.Query)
}

func filterableQueryDisplay(query string) string {
	return query + "_"
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

func focusActionsAfterQueryChange(items []FilterablePickerItem, cfg FilterablePickerConfig, query string) []update.Action {
	filtered := filterablePickerItems(items, query)
	if len(filtered) > 0 {
		return []update.Action{interaction.FocusKeyAction{Key: filterablePickerItemFocusKey(filtered, cfg, 0)}}
	}
	if cfg.OnNoMatchFocus != nil {
		return cfg.OnNoMatchFocus()
	}
	return nil
}

func filterablePickerItemFocusKey(filtered []FilterablePickerItem, cfg FilterablePickerConfig, index int) string {
	if index < 0 {
		index = 0
	}
	if index >= len(filtered) {
		index = len(filtered) - 1
	}
	if index >= 0 && index < len(filtered) {
		if key := strings.TrimSpace(filtered[index].Key); key != "" {
			return key
		}
	}
	return FilterablePickerFocusKey(cfg.KeyPrefix, index)
}

func defaultFilterablePickerOptionRow(showSelected bool) func(item FilterablePickerItem, onCancel func() []update.Action) view.ViewSpec[state.Model] {
	return func(item FilterablePickerItem, onCancel func() []update.Action) view.ViewSpec[state.Model] {
		return toolkitviews.ListItemRow[state.Model](
			toolkitviews.InsetLabel(strings.TrimSpace(item.Label), 3),
			item.Selected,
			showSelected,
			true,
			item.OnChoose,
			onCancel,
		)
	}
}

func ChoicePickerOptionRow(showSelected bool) func(item FilterablePickerItem, onCancel func() []update.Action) view.ViewSpec[state.Model] {
	return defaultFilterablePickerOptionRow(showSelected)
}
