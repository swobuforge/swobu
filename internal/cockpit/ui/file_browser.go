package ui

import (
	"fmt"
	"sort"

	tui "github.com/grindlemire/go-tui"
)

const fileBrowserDefaultVisibleRows = 7

type FileBrowserEntry struct {
	Name  string
	Path  string
	IsDir bool
}

type FileBrowserListing struct {
	Path    string
	Parent  string
	Entries []FileBrowserEntry
}

type FileBrowserBrowse func(string) (FileBrowserListing, error)

// FileBrowserError carries safe operator-facing browse failure copy across the
// feature boundary without teaching the generic browser about its cause.
type FileBrowserError struct {
	Message string
}

func (e FileBrowserError) Error() string              { return e.Message }
func (e FileBrowserError) FileBrowserMessage() string { return e.Message }

// FileBrowser renders an opaque hierarchy supplied by the resource owner.
// It filters names locally but never interprets or constructs entry paths.
type FileBrowser struct {
	ID          string
	Title       string
	CurrentDir  *tui.State[string]
	Query       *tui.State[string]
	Error       *tui.State[string]
	Loading     *tui.State[bool]
	Browse      FileBrowserBrowse
	OnSelect    func(string)
	OnCancel    func()
	AutoFocus   bool
	VisibleRows int
	entries     []FileBrowserEntry
	list        *ChoiceList
	app         *tui.App
	generation  uint64
	pendingPath string
}

func NewFileBrowser(id, title, initialPath string, browse FileBrowserBrowse, onSelect func(string), onCancel func()) *FileBrowser {
	b := &FileBrowser{
		ID: id, Title: title, CurrentDir: tui.NewState(initialPath), Query: tui.NewState(""),
		Error: tui.NewState(""), Loading: tui.NewState(false), Browse: browse,
		OnSelect: onSelect, OnCancel: onCancel, VisibleRows: fileBrowserDefaultVisibleRows,
		pendingPath: initialPath,
	}
	b.list = NewChoiceList(b.Query)
	b.configureList()
	return b
}

func (b *FileBrowser) BindApp(app *tui.App) {
	if b.app == app {
		return
	}
	b.CurrentDir.BindApp(app)
	b.Error.BindApp(app)
	b.Loading.BindApp(app)
	b.app = app
	b.configureList()
	b.list.BindApp(app)
	b.Navigate(b.pendingPath)
}

func (b *FileBrowser) UnbindApp() {
	b.generation++
	b.app = nil
}

func (b *FileBrowser) UpdateProps(fresh tui.Component) {
	f, ok := fresh.(*FileBrowser)
	if !ok {
		return
	}
	b.ID, b.Title, b.Browse = f.ID, f.Title, f.Browse
	b.OnSelect, b.OnCancel = f.OnSelect, f.OnCancel
	b.AutoFocus, b.VisibleRows = f.AutoFocus, f.VisibleRows
	b.configureList()
}

func (b *FileBrowser) Navigate(path string) {
	b.Query.Set("")
	b.choiceList().ResetProjection()
	b.pendingPath = path
	b.generation++
	generation := b.generation
	b.Loading.Set(true)
	b.Error.Set("")
	b.entries = nil
	b.configureList()
	if b.app == nil {
		return
	}
	browse, app := b.Browse, b.app
	go func() {
		var listing FileBrowserListing
		var err error
		if browse == nil {
			err = fmt.Errorf("credential file browser is unavailable")
		} else {
			listing, err = browse(path)
		}
		select {
		case <-app.StopCh():
			return
		default:
		}
		app.QueueUpdate(func() { b.completeBrowse(generation, listing, err) })
	}()
}

func (b *FileBrowser) completeBrowse(generation uint64, listing FileBrowserListing, err error) {
	if generation != b.generation {
		return
	}
	b.Loading.Set(false)
	if err != nil {
		message := "could not browse directory"
		if presentable, ok := err.(interface{ FileBrowserMessage() string }); ok {
			message = presentable.FileBrowserMessage()
		}
		b.Error.Set(message)
		b.entries = nil
		b.configureList()
		return
	}
	b.CurrentDir.Set(listing.Path)
	b.pendingPath = listing.Path
	b.Error.Set("")
	b.entries = append([]FileBrowserEntry(nil), listing.Entries...)
	if listing.Parent != "" {
		b.entries = append(b.entries, FileBrowserEntry{Name: "../", Path: listing.Parent, IsDir: true})
	}
	sort.SliceStable(b.entries, func(i, j int) bool {
		if b.entries[i].Name == "../" {
			return true
		}
		if b.entries[j].Name == "../" {
			return false
		}
		if b.entries[i].IsDir != b.entries[j].IsDir {
			return b.entries[i].IsDir
		}
		return b.entries[i].Name < b.entries[j].Name
	})
	b.configureList()
}

type BrowserRow struct {
	Name, Path string
	IsDir      bool
	Index      int
}

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
	rows := make([]BrowserRow, 0, len(win.Rows))
	for _, row := range win.Rows {
		rows = append(rows, b.browserRowFromChoice(row))
	}
	return BrowserWindow{CurrentDir: b.CurrentDir.Get(), Query: b.Query.Get(), Rows: rows, TotalRows: win.TotalRows, ShownRows: win.ShownRows, HasError: b.Error.Get() != "", ErrorText: b.Error.Get()}
}

func (b *FileBrowser) choiceItems() []ChoiceItem {
	items := make([]ChoiceItem, 0, len(b.entries))
	for _, entry := range b.entries {
		items = append(items, b.choiceItem(BrowserRow{Name: entry.Name, Path: entry.Path, IsDir: entry.IsDir, Index: len(items)}))
	}
	return items
}

func (b *FileBrowser) choiceItem(row BrowserRow) ChoiceItem {
	rowCopy := row
	return ChoiceItem{Key: fileBrowserRowKey(rowCopy, rowCopy.Index), Label: fileBrowserDisplayName(rowCopy), Value: rowCopy.Name, Action: fileBrowserActionLabel(rowCopy), AlwaysVisible: rowCopy.Name == "../", Choose: func() { b.activateRow(rowCopy) }}
}

func (b *FileBrowser) browserRowFromChoice(row ChoiceRowModel) BrowserRow {
	for index, entry := range b.entries {
		if fileBrowserRowKey(BrowserRow{Name: entry.Name}, index) == row.Item.Key {
			return BrowserRow{Name: entry.Name, Path: entry.Path, IsDir: entry.IsDir, Index: row.Index}
		}
	}
	return BrowserRow{Name: row.Item.Value, IsDir: row.Item.Action == "open ↵", Index: row.Index}
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
		b.Navigate(row.Path)
		return
	}
	if b.OnSelect != nil {
		b.OnSelect(row.Path)
	}
}

func (b *FileBrowser) onEscape(_ tui.KeyEvent) {
	if b.OnCancel != nil {
		b.OnCancel()
	}
}

// KeyMap keeps Escape available while an asynchronous load has no rows to own
// focused dispatch. Once rows exist, the browser remains the scope owner.
func (b *FileBrowser) KeyMap() tui.KeyMap {
	return tui.KeyMap{tui.OnPreemptStop(tui.KeyEscape, b.onEscape)}
}

func (b *FileBrowser) configureList() {
	if b.list == nil || b.list.Query != b.Query {
		b.list = NewChoiceList(b.Query)
	}
	b.list.VisibleRows = b.VisibleRows
	if b.list.VisibleRows <= 0 {
		b.list.VisibleRows = fileBrowserDefaultVisibleRows
	}
	b.list.AutoFocus, b.list.QueryEditing, b.list.OnEscape = b.AutoFocus, true, b.onEscape
	b.list.SetItems(b.choiceItems())
}

func (b *FileBrowser) choiceList() *ChoiceList {
	if b.list == nil {
		b.configureList()
	}
	return b.list
}

var (
	_ tui.Component    = (*FileBrowser)(nil)
	_ tui.AppBinder    = (*FileBrowser)(nil)
	_ tui.AppUnbinder  = (*FileBrowser)(nil)
	_ tui.PropsUpdater = (*FileBrowser)(nil)
)
