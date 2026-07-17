package ui

import (
	"fmt"
	"os"
	"strings"
	"testing"

	tui "github.com/grindlemire/go-tui"
	"github.com/swobuforge/swobu/internal/cockpit/testkit"
)

func TestFileBrowser_WindowListsEntries(t *testing.T) {
	readDir := func(string) ([]FileBrowserEntry, error) {
		return []FileBrowserEntry{
			{Name: "alpha.txt", IsDir: false},
			{Name: "beta", IsDir: true},
		}, nil
	}
	b := NewFileBrowser("fb", "files", "/home/demo", readDir, nil, nil)
	win := b.Window()

	if win.CurrentDir != "/home/demo" {
		t.Fatalf("currentDir = %q, want /home/demo", win.CurrentDir)
	}
	if len(win.Rows) != 3 {
		t.Fatalf("row count = %d, want 3", len(win.Rows))
	}
	if win.Rows[0].Name != "../" {
		t.Fatalf("first row = %+v, want parent entry", win.Rows[0])
	}
	if win.Rows[1].Name != "beta" || !win.Rows[1].IsDir {
		t.Fatalf("second row = %+v, want beta dir", win.Rows[1])
	}
	if win.Rows[2].Name != "alpha.txt" || win.Rows[2].IsDir {
		t.Fatalf("third row = %+v, want alpha.txt file", win.Rows[2])
	}
}

func TestFileBrowser_WindowProjectsVisibleRows(t *testing.T) {
	readDir := func(string) ([]FileBrowserEntry, error) {
		return manyFileBrowserEntries(12), nil
	}
	b := NewFileBrowser("fb", "files", "/home/demo", readDir, nil, nil)
	win := b.Window()

	if len(win.Rows) != fileBrowserDefaultVisibleRows {
		t.Fatalf("projected rows = %d, want %d", len(win.Rows), fileBrowserDefaultVisibleRows)
	}
	if win.ShownRows != fileBrowserDefaultVisibleRows {
		t.Fatalf("shown rows = %d, want %d", win.ShownRows, fileBrowserDefaultVisibleRows)
	}
	if win.TotalRows != 13 {
		t.Fatalf("total rows = %d, want parent plus 12 entries", win.TotalRows)
	}
	if got := fileBrowserCountLabel(win.ShownRows, win.TotalRows); got != "7 of 13 shown" {
		t.Fatalf("count label = %q, want bounded shown count", got)
	}
}

func TestFileBrowser_WindowFiltersEntriesAndKeepsParent(t *testing.T) {
	readDir := func(string) ([]FileBrowserEntry, error) {
		return []FileBrowserEntry{
			{Name: "alpha.json", IsDir: false},
			{Name: "beta.txt", IsDir: false},
			{Name: "secrets", IsDir: true},
		}, nil
	}
	b := NewFileBrowser("fb", "files", "/home/demo", readDir, nil, nil)
	b.Query.Set("sec")

	win := b.Window()
	if len(win.Rows) != 2 {
		t.Fatalf("rows = %+v, want parent plus secrets", win.Rows)
	}
	if win.Rows[0].Name != "../" {
		t.Fatalf("first row = %+v, want parent entry", win.Rows[0])
	}
	if win.Rows[1].Name != "secrets" || !win.Rows[1].IsDir {
		t.Fatalf("second row = %+v, want secrets dir", win.Rows[1])
	}
}

func TestFileBrowser_NavigateClearsSearch(t *testing.T) {
	readDir := func(string) ([]FileBrowserEntry, error) {
		return []FileBrowserEntry{{Name: "sub", IsDir: true}}, nil
	}
	b := NewFileBrowser("fb", "files", "/home/demo", readDir, nil, nil)
	b.Query.Set("sub")
	b.choiceList().WindowStart.Set(2)
	b.choiceList().FocusKey.Set("sub")

	b.activateRow(BrowserRow{Name: "sub", IsDir: true})
	if b.Query.Get() != "" {
		t.Fatalf("query after navigate = %q, want empty", b.Query.Get())
	}
	if b.choiceList().WindowStart.Get() != 0 {
		t.Fatalf("window start after navigate = %d, want reset", b.choiceList().WindowStart.Get())
	}
	if b.choiceList().FocusKey.Get() != "" {
		t.Fatalf("focus row key after navigate = %q, want reset", b.choiceList().FocusKey.Get())
	}
	if b.CurrentDir.Get() == "/home/demo" {
		t.Fatalf("dir did not navigate, still %q", b.CurrentDir.Get())
	}
}

func TestFileBrowser_ActivateFileCallsOnSelect(t *testing.T) {
	var selected string
	readDir := func(string) ([]FileBrowserEntry, error) {
		return []FileBrowserEntry{{Name: "secret.json", IsDir: false}}, nil
	}
	b := NewFileBrowser("fb", "files", "/etc", readDir, func(path string) { selected = path }, nil)

	b.activateRow(BrowserRow{Name: "secret.json", IsDir: false})
	if !strings.Contains(selected, "secret.json") {
		t.Fatalf("selected = %q, want path containing secret.json", selected)
	}
}

func TestFileBrowser_ActivateParentNavigatesUp(t *testing.T) {
	readDir := func(string) ([]FileBrowserEntry, error) {
		return nil, nil
	}
	b := NewFileBrowser("fb", "files", "/home/demo", readDir, nil, nil)

	b.activateRow(BrowserRow{Name: "../", IsDir: true})
	if b.CurrentDir.Get() != "/home" {
		t.Fatalf("dir = %q, want /home", b.CurrentDir.Get())
	}
}

func TestFileBrowser_RefreshDirShowsError(t *testing.T) {
	readDir := func(string) ([]FileBrowserEntry, error) {
		return nil, os.ErrPermission
	}
	b := NewFileBrowser("fb", "files", "/root", readDir, nil, nil)
	win := b.Window()

	if !win.HasError {
		t.Fatal("expected HasError = true")
	}
	if !strings.Contains(win.ErrorText, "permission") {
		t.Fatalf("error text = %q, want permission-related", win.ErrorText)
	}
}

func TestFileBrowser_EmptyDirShowsOnlyParent(t *testing.T) {
	readDir := func(string) ([]FileBrowserEntry, error) {
		return nil, nil
	}
	b := NewFileBrowser("fb", "files", "/empty", readDir, nil, nil)
	win := b.Window()

	if len(win.Rows) != 1 {
		t.Fatalf("rows = %v, want just ../", win.Rows)
	}
}

func TestFileBrowser_OSReadDirIncludesDotfiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/visible.txt", []byte("a"), 0644)
	os.WriteFile(dir+"/._hidden", []byte("b"), 0644)
	os.Mkdir(dir+"/.git", 0755)

	entries, err := OSReadDir(dir)
	if err != nil {
		t.Fatalf("OSReadDir: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %v, want visible files and dotfiles", entries)
	}
	want := []FileBrowserEntry{
		{Name: "._hidden", IsDir: false},
		{Name: ".git", IsDir: true},
		{Name: "visible.txt", IsDir: false},
	}
	for i := range want {
		if entries[i] != want[i] {
			t.Fatalf("entry[%d] = %+v, want %+v; all entries = %+v", i, entries[i], want[i], entries)
		}
	}
}

func TestFileBrowser_UpdatePropsPreservesDirectoryAndQuery(t *testing.T) {
	readDir := func(string) ([]FileBrowserEntry, error) {
		return []FileBrowserEntry{{Name: "a.txt", IsDir: false}}, nil
	}
	b := NewFileBrowser("fb", "files", "/", readDir, nil, nil)
	b.Query.Set("a")

	fresh := NewFileBrowser("fb", "files", "/other", readDir, nil, nil)
	b.UpdateProps(fresh)

	win := b.Window()
	if win.CurrentDir != "/" {
		t.Fatalf("UpdateProps should not change CurrentDir, got %q", win.CurrentDir)
	}
	if b.Query.Get() != "a" {
		t.Fatalf("UpdateProps should preserve query, got %q", b.Query.Get())
	}
}

func TestFileBrowser_UpdatePropsRefreshesBrowserID(t *testing.T) {
	readDir := func(string) ([]FileBrowserEntry, error) {
		return []FileBrowserEntry{{Name: "a.txt", IsDir: false}}, nil
	}
	b := NewFileBrowser("credential-browser", "files", "/", readDir, nil, nil)
	fresh := NewFileBrowser("model-browser", "files", "/", readDir, nil, nil)

	b.UpdateProps(fresh)

	if got := b.ID; got != "model-browser" {
		t.Fatalf("browser ID after update = %q, want model-browser", got)
	}
}

func TestFileBrowser_NoReadDirShowsError(t *testing.T) {
	b := NewFileBrowser("fb", "files", "/", nil, nil, nil)
	win := b.Window()
	if !win.HasError {
		t.Fatal("expected HasError when ReadDir is nil")
	}
}

func TestFileBrowser_EntryComponentIDIncludesBrowserID(t *testing.T) {
	readDir := func(string) ([]FileBrowserEntry, error) {
		return []FileBrowserEntry{{Name: "secret.json", IsDir: false}}, nil
	}
	b := NewFileBrowser("credential-browser", "files", "/", readDir, nil, nil)
	list := b.choiceList()
	row := ChoiceRowModel{Item: ChoiceItem{Key: "secret.json", Label: "secret.json"}}

	entry := FileBrowserEntryComponent(b, list, row, false)

	if got, want := entry.target.Props().ID, "credential-browser:entry:secret.json"; got != want {
		t.Fatalf("entry component ID = %q, want %q", got, want)
	}
}

func TestFileBrowser_RenderProducesSelectableRows(t *testing.T) {
	readDir := func(string) ([]FileBrowserEntry, error) {
		return []FileBrowserEntry{{Name: "demo.txt", IsDir: false}}, nil
	}
	b := NewFileBrowser("fb", "files", "/", readDir, nil, nil)
	b.AutoFocus = true

	rendered := testkit.RenderMountedTrimmed(t, b, 120, 12)
	if !strings.Contains(rendered, "> ../") {
		t.Fatalf("first file-browser entry should be selected:\n%s", rendered)
	}
	if !strings.Contains(rendered, "demo.txt") {
		t.Fatalf("file entry missing:\n%s", rendered)
	}
	if !strings.Contains(rendered, "2 of 2 shown") {
		t.Fatalf("count missing:\n%s", rendered)
	}
}

func TestFileBrowser_RenderDoesNotRepairWindowStart(t *testing.T) {
	readDir := func(string) ([]FileBrowserEntry, error) {
		return manyFileBrowserEntries(8), nil
	}
	b := NewFileBrowser("fb", "files", "/", readDir, nil, nil)
	h, err := testkit.NewHarness(b)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	list := b.choiceList()
	list.VisibleRows = 3
	list.SetItems(choiceListItems(8))
	list.WindowStart.Set(5)
	list.Items = choiceListItems(4)

	rendered := h.Frame()

	if got, want := list.WindowStart.Get(), 5; got != want {
		t.Fatalf("window start after render = %d, want %d\n%s", got, want, rendered)
	}
	if !strings.Contains(rendered, "Item 1") {
		t.Fatalf("render should still use bounded projection:\n%s", rendered)
	}
}

func TestFileBrowser_RenderBoundsLongDirectory(t *testing.T) {
	readDir := func(string) ([]FileBrowserEntry, error) {
		return manyFileBrowserEntries(12), nil
	}
	b := NewFileBrowser("fb", "files", "/", readDir, nil, nil)
	b.AutoFocus = true

	rendered := testkit.RenderMountedTrimmed(t, b, 120, 20)
	if strings.Count(rendered, "select ↵") != 6 {
		t.Fatalf("rendered file rows = %d, want first projected page only:\n%s", strings.Count(rendered, "select ↵"), rendered)
	}
	if strings.Contains(rendered, "file-011.txt") {
		t.Fatalf("render should not mount entries beyond projection:\n%s", rendered)
	}
	if !strings.Contains(rendered, "7 of 13 shown") {
		t.Fatalf("bounded count missing:\n%s", rendered)
	}
}

func TestFileBrowser_AppLoop_KeyDownMovesGlobalSelectionToNextEntry(t *testing.T) {
	readDir := func(string) ([]FileBrowserEntry, error) {
		return []FileBrowserEntry{{Name: "demo.txt", IsDir: false}}, nil
	}
	b := NewFileBrowser("fb", "files", "/", readDir, nil, nil)
	b.AutoFocus = true

	h, err := testkit.NewHarness(b)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})

	frame := h.Frame()
	if !strings.Contains(frame, "> demo.txt") {
		t.Fatalf("Down should select the file row through global selection:\n%s", frame)
	}
}

func TestFileBrowser_AppLoop_KeyDownAtProjectionEndRevealsNextEntry(t *testing.T) {
	readDir := func(string) ([]FileBrowserEntry, error) {
		return manyFileBrowserEntries(12), nil
	}
	b := NewFileBrowser("fb", "files", "/", readDir, nil, nil)
	b.AutoFocus = true

	h, err := testkit.NewHarness(b)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	for range fileBrowserDefaultVisibleRows {
		h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	}

	frame := h.Frame()
	if !strings.Contains(frame, "> file-006.txt") {
		t.Fatalf("edge Down should reveal and select the next file row:\n%s", frame)
	}
	if strings.Contains(frame, "../") {
		t.Fatalf("parent row should have scrolled out of the projected file window:\n%s", frame)
	}
	if !strings.Contains(frame, "7 of 13 shown") {
		t.Fatalf("bounded count should remain stable after projection move:\n%s", frame)
	}
}

func TestFileBrowser_AppLoop_PageDownMovesBoundedProjection(t *testing.T) {
	readDir := func(string) ([]FileBrowserEntry, error) {
		return manyFileBrowserEntries(12), nil
	}
	b := NewFileBrowser("fb", "files", "/", readDir, nil, nil)
	b.AutoFocus = true

	h, err := testkit.NewHarness(b)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyPageDown})

	frame := h.Frame()
	if !strings.Contains(frame, "> file-006.txt") {
		t.Fatalf("PageDown should select the next bounded file page:\n%s", frame)
	}
	if strings.Contains(frame, "../") {
		t.Fatalf("PageDown should advance the file list projection before body scroll:\n%s", frame)
	}
	if !strings.Contains(frame, "7 of 13 shown") {
		t.Fatalf("bounded count should remain stable after PageDown:\n%s", frame)
	}
}

func TestFileBrowser_AppLoop_PageUpMovesBoundedProjection(t *testing.T) {
	readDir := func(string) ([]FileBrowserEntry, error) {
		return manyFileBrowserEntries(12), nil
	}
	b := NewFileBrowser("fb", "files", "/", readDir, nil, nil)
	b.AutoFocus = true

	h, err := testkit.NewHarness(b)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyPageDown})

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyPageUp})

	frame := h.Frame()
	if !strings.Contains(frame, "> ../") {
		t.Fatalf("PageUp should return to the previous bounded file page:\n%s", frame)
	}
	if strings.Contains(frame, "file-006.txt") {
		t.Fatalf("PageUp should move the file list projection back:\n%s", frame)
	}
	if !strings.Contains(frame, "7 of 13 shown") {
		t.Fatalf("bounded count should remain stable after PageUp:\n%s", frame)
	}
}

func TestFileBrowser_AppLoop_KeyEnterSelectsFocusedFile(t *testing.T) {
	var selected string
	readDir := func(string) ([]FileBrowserEntry, error) {
		return []FileBrowserEntry{{Name: "demo.txt", IsDir: false}}, nil
	}
	b := NewFileBrowser("fb", "files", "/", readDir, func(path string) { selected = path }, nil)
	b.AutoFocus = true

	h, err := testkit.NewHarness(b)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()
	h.Open()

	h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})

	if !strings.Contains(selected, "demo.txt") {
		t.Fatalf("selected = %q, want demo.txt", selected)
	}
}

func TestFileBrowser_AppLoop_KeyEscapeCancels(t *testing.T) {
	var cancelled bool
	readDir := func(string) ([]FileBrowserEntry, error) { return nil, nil }
	b := NewFileBrowser("fb", "files", "/", readDir, nil, func() { cancelled = true })
	b.AutoFocus = true

	h, err := testkit.NewHarness(b)
	if err != nil {
		t.Fatalf("NewHarness: %v", err)
	}
	defer h.Close()

	h.Open()
	h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})

	if !cancelled {
		t.Fatal("OnCancel not fired via Escape dispatch")
	}
}

func manyFileBrowserEntries(n int) []FileBrowserEntry {
	entries := make([]FileBrowserEntry, 0, n)
	for i := range n {
		entries = append(entries, FileBrowserEntry{Name: fmt.Sprintf("file-%03d.txt", i), IsDir: false})
	}
	return entries
}
