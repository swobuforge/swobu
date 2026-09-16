package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tui "github.com/grindlemire/go-tui"
	testkit "github.com/swobuforge/swobu/internal/testkit/cockpittestkit"
)

func listing(path, parent string, entries ...FileBrowserEntry) FileBrowserListing {
	return FileBrowserListing{Path: path, Parent: parent, Entries: entries}
}

func TestFileBrowserProjectsOwnerSuppliedOpaquePaths(t *testing.T) {
	b := NewFileBrowser("fb", "credential file", `seed`, nil, nil, nil)
	b.completeBrowse(0, listing(`C:\Users\operator`, `C:\Users`,
		FileBrowserEntry{Name: "key", Path: `C:\Users\operator\key`},
		FileBrowserEntry{Name: "config", Path: `C:\Users\operator\config`, IsDir: true},
	), nil)
	win := b.Window()
	if win.CurrentDir != `C:\Users\operator` || len(win.Rows) != 3 {
		t.Fatalf("window = %#v", win)
	}
	if win.Rows[0].Path != `C:\Users` || win.Rows[1].Path != `C:\Users\operator\config` || win.Rows[2].Path != `C:\Users\operator\key` {
		t.Fatalf("rows changed opaque paths: %#v", win.Rows)
	}
}

func TestFileBrowserDirectoryNavigationUsesReturnedPath(t *testing.T) {
	b := NewFileBrowser("fb", "files", "", nil, nil, nil)
	b.activateRow(BrowserRow{Name: "foreign", Path: `/home/operator/foreign`, IsDir: true})
	if b.pendingPath != `/home/operator/foreign` {
		t.Fatalf("pending path = %q", b.pendingPath)
	}
}

func TestFileBrowserFileSelectionUsesReturnedPath(t *testing.T) {
	var selected string
	b := NewFileBrowser("fb", "files", "", nil, func(path string) { selected = path }, nil)
	b.activateRow(BrowserRow{Name: "key", Path: `/home/operator/key`})
	if selected != `/home/operator/key` {
		t.Fatalf("selected = %q", selected)
	}
}

func TestFileBrowserIgnoresStaleBrowseCompletion(t *testing.T) {
	b := NewFileBrowser("fb", "files", "", nil, nil, nil)
	b.generation = 2
	b.completeBrowse(2, listing("B", "", FileBrowserEntry{Name: "b", Path: "B/b"}), nil)
	b.completeBrowse(1, listing("A", "", FileBrowserEntry{Name: "a", Path: "A/a"}), nil)
	if got := b.CurrentDir.Get(); got != "B" {
		t.Fatalf("current dir = %q, want B", got)
	}
}

func TestFileBrowserLoadingAndErrorProjection(t *testing.T) {
	b := NewFileBrowser("fb", "credential file", "", nil, nil, nil)
	b.Navigate("")
	if !b.Loading.Get() {
		t.Fatal("Navigate must enter loading")
	}
	b.completeBrowse(b.generation, FileBrowserListing{}, errors.New("denied"))
	if b.Loading.Get() || b.Error.Get() != "could not browse directory" {
		t.Fatalf("loading=%v error=%q", b.Loading.Get(), b.Error.Get())
	}
}

func TestFileBrowserFiltersNamesWithoutChangingPaths(t *testing.T) {
	b := NewFileBrowser("fb", "files", "", nil, nil, nil)
	b.completeBrowse(0, listing("/", "", FileBrowserEntry{Name: "secret", Path: `C:\opaque\secret`}, FileBrowserEntry{Name: "other", Path: "/foreign/other"}), nil)
	b.Query.Set("sec")
	win := b.Window()
	if len(win.Rows) != 1 || win.Rows[0].Path != `C:\opaque\secret` {
		t.Fatalf("rows = %#v", win.Rows)
	}
}

func TestFileBrowserSearchesNamesNotOpaquePaths(t *testing.T) {
	b := NewFileBrowser("fb", "files", "", nil, nil, nil)
	b.completeBrowse(0, listing("/home/secret-parent", "", FileBrowserEntry{Name: "key.json", Path: "/home/secret-parent/key.json"}), nil)
	b.Query.Set("secret-parent")
	if rows := b.Window().Rows; len(rows) != 0 {
		t.Fatalf("parent path leaked into filename search: %#v", rows)
	}
}

func TestFileBrowserErrorCopyDoesNotExposeBackendText(t *testing.T) {
	b := NewFileBrowser("fb", "files", "", nil, nil, nil)
	b.completeBrowse(0, FileBrowserListing{}, errors.New("sensitive absolute path"))
	if strings.Contains(b.Error.Get(), "sensitive") {
		t.Fatalf("error leaked backend text: %q", b.Error.Get())
	}
}

func TestFileBrowser_AppLoop_AsyncLoadFocusesFirstRow(t *testing.T) {
	b := mountedFileBrowser(t, func(string) (FileBrowserListing, error) {
		return listing("/opaque", "", FileBrowserEntry{Name: "first.key", Path: "/opaque/first.key"}), nil
	}, nil, nil)
	frame := waitForFileBrowserFrame(t, b.h, func(frame string) bool { return strings.Contains(frame, "first.key") })
	if !strings.Contains(frame, "> first.key") {
		t.Fatalf("first asynchronously loaded row is not selected:\n%s", frame)
	}
}

func TestFileBrowser_AppLoop_DownEnterSelectsExactReturnedPath(t *testing.T) {
	selected := make(chan string, 1)
	b := mountedFileBrowser(t, func(string) (FileBrowserListing, error) {
		return listing("/opaque", "",
			FileBrowserEntry{Name: "first.key", Path: `C:\opaque\first.key`},
			FileBrowserEntry{Name: "second.key", Path: `C:\opaque\second.key`},
		), nil
	}, func(path string) { selected <- path }, nil)
	waitForFileBrowserFrame(t, b.h, func(frame string) bool { return strings.Contains(frame, "> first.key") })
	b.h.DispatchKey(tui.KeyEvent{Key: tui.KeyDown})
	b.h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	select {
	case got := <-selected:
		if got != `C:\opaque\second.key` {
			t.Fatalf("selected = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("mounted Enter did not select a file")
	}
}

func TestFileBrowser_AppLoop_EnterDirectoryBrowsesExactReturnedPath(t *testing.T) {
	calls := make(chan string, 2)
	b := mountedFileBrowser(t, func(path string) (FileBrowserListing, error) {
		calls <- path
		if path == "seed" {
			return listing("root", "", FileBrowserEntry{Name: "foreign", Path: `/home/operator/foreign`, IsDir: true}), nil
		}
		return listing(path, "", FileBrowserEntry{Name: "child", Path: path + "/child"}), nil
	}, nil, nil)
	waitForFileBrowserFrame(t, b.h, func(frame string) bool { return strings.Contains(frame, "> foreign/") })
	b.h.DispatchKey(tui.KeyEvent{Key: tui.KeyEnter})
	waitForFileBrowserFrame(t, b.h, func(frame string) bool { return strings.Contains(frame, "child") })
	<-calls
	if got := <-calls; got != `/home/operator/foreign` {
		t.Fatalf("directory browse path = %q", got)
	}
}

func TestFileBrowser_AppLoop_EscapeCancelsLoadedAndLoading(t *testing.T) {
	t.Run("loaded", func(t *testing.T) {
		cancelled := make(chan struct{}, 1)
		b := mountedFileBrowser(t, func(string) (FileBrowserListing, error) {
			return listing("root", "", FileBrowserEntry{Name: "key", Path: "opaque-key"}), nil
		}, nil, func() { cancelled <- struct{}{} })
		waitForFileBrowserFrame(t, b.h, func(frame string) bool { return strings.Contains(frame, "> key") })
		b.h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})
		waitForSignal(t, cancelled, "loaded browser Escape")
	})

	t.Run("loading", func(t *testing.T) {
		release := make(chan struct{})
		cancelled := make(chan struct{}, 1)
		b := mountedFileBrowser(t, func(string) (FileBrowserListing, error) {
			<-release
			return listing("root", ""), nil
		}, nil, func() { cancelled <- struct{}{} })
		defer close(release)
		frame := waitForFileBrowserFrame(t, b.h, func(frame string) bool { return strings.Contains(frame, "loading…") })
		if !strings.Contains(frame, "loading…") {
			t.Fatalf("browser never entered loading:\n%s", frame)
		}
		b.h.DispatchKey(tui.KeyEvent{Key: tui.KeyEscape})
		waitForSignal(t, cancelled, "loading browser Escape")
	})
}

func TestFileBrowser_AppLoop_PageDownKeepsBoundedProjection(t *testing.T) {
	entries := make([]FileBrowserEntry, 12)
	for i := range entries {
		entries[i] = FileBrowserEntry{Name: fmt.Sprintf("key-%02d", i), Path: fmt.Sprintf("opaque-%02d", i)}
	}
	b := mountedFileBrowser(t, func(string) (FileBrowserListing, error) { return listing("root", "", entries...), nil }, nil, nil)
	waitForFileBrowserFrame(t, b.h, func(frame string) bool { return strings.Contains(frame, "> key-00") })
	b.h.DispatchKey(tui.KeyEvent{Key: tui.KeyPageDown})
	frame := b.h.FrameTrimmed()
	if !strings.Contains(frame, "> key-07") || strings.Contains(frame, "key-00") {
		t.Fatalf("PageDown did not shift the bounded projection:\n%s", frame)
	}
	if !strings.Contains(frame, "7 of 12 shown") {
		t.Fatalf("bounded projection count changed:\n%s", frame)
	}
}

func TestFileBrowser_AppLoop_StaleResponseCannotReplaceNewerListing(t *testing.T) {
	aResult := make(chan struct{})
	aReturned := make(chan struct{}, 1)
	b := mountedFileBrowser(t, func(path string) (FileBrowserListing, error) {
		if path == "seed" {
			<-aResult
			aReturned <- struct{}{}
			return listing("A", "", FileBrowserEntry{Name: "a", Path: "A/a"}), nil
		}
		return listing("B", "", FileBrowserEntry{Name: "b", Path: "B/b"}), nil
	}, nil, nil)
	b.browser.Navigate("B")
	waitForFileBrowserFrame(t, b.h, func(frame string) bool { return strings.Contains(frame, "> b") })
	close(aResult)
	waitForSignal(t, aReturned, "stale A browse return")
	b.h.Frame()
	frame := b.h.FrameTrimmed()
	if b.browser.CurrentDir.Get() != "B" || strings.Contains(frame, "> a") {
		t.Fatalf("stale A replaced B:\n%s", frame)
	}
}

func TestFileBrowserUnbindInvalidatesCompletionAndAllowsRebind(t *testing.T) {
	release := make(chan struct{})
	calls := make(chan struct{}, 2)
	returned := make(chan struct{}, 2)
	b := NewFileBrowser("fb", "files", "seed", func(string) (FileBrowserListing, error) {
		calls <- struct{}{}
		<-release
		returned <- struct{}{}
		return listing("late", "", FileBrowserEntry{Name: "late", Path: "late/file"}), nil
	}, nil, nil)
	b.AutoFocus = true
	h, err := testkit.NewHarness(b)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	h.Open()
	waitForSignal(t, calls, "initial browse")
	b.UnbindApp()
	close(release)
	waitForSignal(t, returned, "detached browse return")
	h.Frame()
	if b.CurrentDir.Get() == "late" {
		t.Fatal("completion after unbind mutated browser state")
	}
	b.BindApp(h.App())
	waitForSignal(t, calls, "rebound browse")
	waitForSignal(t, returned, "rebound browse return")
	frame := waitForFileBrowserFrame(t, h, func(frame string) bool { return strings.Contains(frame, "> late") })
	if b.CurrentDir.Get() != "late" {
		t.Fatalf("rebound completion was not installed:\n%s", frame)
	}
}

type mountedBrowser struct {
	browser *FileBrowser
	h       *testkit.MockAppHarness
}

func mountedFileBrowser(t *testing.T, browse FileBrowserBrowse, selectFile func(string), cancel func()) mountedBrowser {
	t.Helper()
	b := NewFileBrowser("fb", "credential file", "seed", browse, selectFile, cancel)
	b.AutoFocus = true
	h, err := testkit.NewHarnessAt(b, 100, 16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	h.Open()
	return mountedBrowser{browser: b, h: h}
}

func waitForFileBrowserFrame(t *testing.T, h *testkit.MockAppHarness, settled func(string) bool) string {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	var frame string
	for time.Now().Before(deadline) {
		frame = h.FrameTrimmed()
		if settled(frame) {
			return frame
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("file browser did not settle:\n%s", frame)
	return ""
}

func waitForSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}
