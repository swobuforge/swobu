package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	tui "github.com/grindlemire/go-tui"
)

const fileBrowserDefaultVisibleRows = 7

// FileBrowserEntry is one directory listing item.
type FileBrowserEntry struct {
	Name  string
	IsDir bool
}

// FileBrowser is a directory-browsing selectable-list scope.
//
// It owns directory, query, and filesystem loading state. Directory entries
// render as SelectableRow descendants so file choice uses the same Cockpit
// selection cursor as every other row.
type FileBrowser struct {
	ID          string
	Title       string
	CurrentDir  *tui.State[string]
	Query       *tui.State[string]
	Error       *tui.State[string]
	ReadDir     func(string) ([]FileBrowserEntry, error)
	OnSelect    func(string)
	OnCancel    func()
	AutoFocus   bool
	VisibleRows int
	entries     []FileBrowserEntry
	list        *ChoiceList
}

// NewFileBrowser creates a mountable file browser starting at initialDir.
func NewFileBrowser(id, title, initialDir string, readDir func(string) ([]FileBrowserEntry, error), onSelect func(string), onCancel func()) *FileBrowser {
	b := &FileBrowser{
		ID:          id,
		Title:       title,
		CurrentDir:  tui.NewState(initialDir),
		Query:       tui.NewState(""),
		Error:       tui.NewState(""),
		ReadDir:     readDir,
		OnSelect:    onSelect,
		OnCancel:    onCancel,
		VisibleRows: fileBrowserDefaultVisibleRows,
	}
	b.list = NewChoiceList(b.Query)
	b.refresh()
	b.configureList()
	return b
}

// BindApp wires component state to the app lifecycle.
func (b *FileBrowser) BindApp(app *tui.App) {
	b.CurrentDir.BindApp(app)
	b.Error.BindApp(app)
	b.configureList()
	b.list.BindApp(app)
}

// UnbindApp releases the app handle.
func (b *FileBrowser) UnbindApp() {}

func (b *FileBrowser) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*FileBrowser)
	if !ok {
		return
	}
	b.ID = f.ID
	b.Title = f.Title
	b.ReadDir = f.ReadDir
	b.OnSelect = f.OnSelect
	b.OnCancel = f.OnCancel
	b.AutoFocus = f.AutoFocus
	b.VisibleRows = f.VisibleRows
	b.configureList()
}

// Navigate reloads entries for dir and clears the query.
func (b *FileBrowser) Navigate(dir string) {
	b.CurrentDir.Set(dir)
	b.Query.Set("")
	b.choiceList().ResetProjection()
	b.refresh()
}

func (b *FileBrowser) refresh() {
	b.Error.Set("")
	if b.ReadDir == nil {
		b.entries = nil
		b.Error.Set("file browser not wired")
		b.configureList()
		return
	}
	dir := b.CurrentDir.Get()
	if dir == "" {
		dir, _ = os.Getwd()
		if dir == "" {
			dir = "/"
		}
		b.CurrentDir.Set(dir)
	}
	entries, err := b.ReadDir(dir)
	if err != nil {
		b.entries = nil
		b.Error.Set(err.Error())
		b.configureList()
		return
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return entries[i].Name < entries[j].Name
	})
	b.entries = entries
	b.configureList()
}

// BrowserRow is one selectable entry in the browser's projected list.
type BrowserRow struct {
	Name  string
	IsDir bool
	Index int
}

// BrowserWindow is the view-model used by Render.
type BrowserWindow struct {
	CurrentDir string
	Query      string
	Rows       []BrowserRow
	TotalRows  int
	ShownRows  int
	HasError   bool
	ErrorText  string
}

func (b *FileBrowser) Window() BrowserWindow {
	win := b.choiceList().Window()
	projected := make([]BrowserRow, 0, len(win.Rows))
	for _, row := range win.Rows {
		projected = append(projected, browserRowFromChoice(row))
	}
	return BrowserWindow{
		CurrentDir: b.CurrentDir.Get(),
		Query:      b.Query.Get(),
		Rows:       projected,
		TotalRows:  win.TotalRows,
		ShownRows:  win.ShownRows,
		HasError:   b.Error.Get() != "",
		ErrorText:  b.Error.Get(),
	}
}

func (b *FileBrowser) choiceItems() []ChoiceItem {
	items := make([]ChoiceItem, 0, len(b.entries)+1)
	parent := BrowserRow{Name: "../", IsDir: true, Index: 0}
	items = append(items, b.choiceItem(parent))
	for _, e := range b.entries {
		row := BrowserRow{Name: e.Name, IsDir: e.IsDir, Index: len(items)}
		items = append(items, b.choiceItem(row))
	}
	return items
}

func (b *FileBrowser) choiceItem(row BrowserRow) ChoiceItem {
	rowCopy := row
	return ChoiceItem{
		Key:           fileBrowserRowKey(rowCopy, rowCopy.Index),
		Label:         fileBrowserDisplayName(rowCopy),
		Value:         rowCopy.Name,
		Action:        fileBrowserActionLabel(rowCopy),
		AlwaysVisible: rowCopy.Name == "../",
		Choose: func() {
			b.activateRow(rowCopy)
		},
	}
}

func browserRowFromChoice(row ChoiceRowModel) BrowserRow {
	return BrowserRow{Name: row.Item.Value, IsDir: row.Item.Action == "open ↵", Index: row.Index}
}

func (b *FileBrowser) rowPath(row BrowserRow) string {
	if row.Name == "../" {
		dir := b.CurrentDir.Get()
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir
		}
		return parent
	}
	return filepath.Join(b.CurrentDir.Get(), row.Name)
}

func fileBrowserDisplayName(row BrowserRow) string {
	if row.IsDir && row.Name != "../" {
		return row.Name + "/"
	}
	return row.Name
}

func fileBrowserActionLabel(row BrowserRow) string {
	if row.IsDir {
		return "open ↵"
	}
	return "select ↵"
}

func fileBrowserCountLabel(shown, total int) string {
	return fmt.Sprintf("%d of %d shown", shown, total)
}

func fileBrowserRowKey(row BrowserRow, index int) string {
	if row.Name != "" {
		return row.Name
	}
	return fmt.Sprintf("row-%d", index)
}

func (b *FileBrowser) activateRow(row BrowserRow) {
	if row.IsDir {
		b.Navigate(b.rowPath(row))
		return
	}
	if b.OnSelect != nil {
		b.OnSelect(b.rowPath(row))
	}
}

func (b *FileBrowser) onEscape(_ tui.KeyEvent) {
	if b.OnCancel != nil {
		b.OnCancel()
	}
}

func (b *FileBrowser) configureList() {
	if b.list == nil || b.list.Query != b.Query {
		b.list = NewChoiceList(b.Query)
	}
	b.list.VisibleRows = b.VisibleRows
	if b.list.VisibleRows <= 0 {
		b.list.VisibleRows = fileBrowserDefaultVisibleRows
	}
	b.list.AutoFocus = b.AutoFocus
	b.list.QueryEditing = true
	b.list.OnEscape = b.onEscape
	b.list.SetItems(b.choiceItems())
}

func (b *FileBrowser) choiceList() *ChoiceList {
	if b.list == nil {
		b.configureList()
	}
	return b.list
}

// OSReadDir adapts os.ReadDir to FileBrowserEntry while preserving dotfiles.
func OSReadDir(path string) ([]FileBrowserEntry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	result := make([]FileBrowserEntry, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		result = append(result, FileBrowserEntry{Name: name, IsDir: e.IsDir()})
	}
	return result, nil
}

var (
	_ tui.Component    = (*FileBrowser)(nil)
	_ tui.AppBinder    = (*FileBrowser)(nil)
	_ tui.PropsUpdater = (*FileBrowser)(nil)
)
