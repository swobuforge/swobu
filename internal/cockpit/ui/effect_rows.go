package ui

import (
	"fmt"
	"os"
	"sync"

	tui "github.com/grindlemire/go-tui"
)

// OpenURL opens url in the system browser and returns any activation error.
// The platform adapter owns suppressing child-process output so terminal
// surfaces may decide whether an error needs an operator-visible transition.
func OpenURL(url string) error {
	open, _, _ := currentEffectHooks()
	return open(url)
}

// CopyToClipboard attempts to write text to the system clipboard. On failure
// it falls back to a temp file copy and returns the resulting status.
func CopyToClipboard(text string) CopyResult {
	_, write, writeTemp := currentEffectHooks()
	if ok, _ := write(text); ok {
		return CopyResult{Status: CopyOK}
	}
	path, err := writeTemp("", "swobu-diagnostics-", text)
	if err != nil {
		return CopyResult{Status: CopyFailed}
	}
	return CopyResult{Status: CopySavedFile, Path: path}
}

// CopyResult describes the outcome of a clipboard copy attempt.
type CopyResult struct {
	Status CopyStatus
	Path   string // valid when Status == CopySavedFile
}

// CopyStatus indicates which path the copy operation took.
type CopyStatus int

const (
	CopyOK        CopyStatus = iota // written to system clipboard
	CopySavedFile                   // wrote to fallback temp file at Path
	CopyFailed                      // both paths failed
)

func (r CopyResult) ErrorForDisplay() string {
	switch r.Status {
	case CopySavedFile:
		return fmt.Sprintf("saved to %s", r.Path)
	case CopyFailed:
		return "failed · run swobu doctor --copy"
	default:
		return ""
	}
}

// browserOpen and clipboardWrite are platform effect stubs overridden at link
// time by build tags or init-time wiring. Default no-op keeps packages that
// import this file compilable on headless test targets.
var (
	effectHooksMu      sync.RWMutex
	browserOpen        = defaultBrowserOpen
	clipboardWrite     = defaultClipboardWrite
	clipboardWriteTemp = defaultClipboardWriteTemp
)

func defaultBrowserOpen(string) error {
	return fmt.Errorf("browser open not wired")
}

func defaultClipboardWrite(string) (bool, error) {
	return false, fmt.Errorf("clipboard write not wired")
}

func defaultClipboardWriteTemp(dir, prefix, text string) (string, error) {
	f, err := os.CreateTemp(dir, prefix+"*.txt")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(text); err != nil {
		_ = f.Close()
		return "", err
	}
	return f.Name(), f.Close()
}

func currentEffectHooks() (
	func(string) error,
	func(string) (bool, error),
	func(dir, prefix, text string) (string, error),
) {
	effectHooksMu.RLock()
	defer effectHooksMu.RUnlock()
	return browserOpen, clipboardWrite, clipboardWriteTemp
}

// RegisterEffectHooks wires platform-specific implementations for the package.
// The adapter calls this once at process startup. Tests may defer the returned
// cleanup to restore the previous hooks and avoid order-dependent global state.
func RegisterEffectHooks(open func(string) error, write func(string) (bool, error), writeTemp func(dir, prefix, text string) (string, error)) func() {
	effectHooksMu.Lock()
	prevOpen, prevWrite, prevWriteTemp := browserOpen, clipboardWrite, clipboardWriteTemp
	if open != nil {
		browserOpen = open
	}
	if write != nil {
		clipboardWrite = write
	}
	if writeTemp != nil {
		clipboardWriteTemp = writeTemp
	}
	effectHooksMu.Unlock()

	return func() {
		effectHooksMu.Lock()
		browserOpen = prevOpen
		clipboardWrite = prevWrite
		clipboardWriteTemp = prevWriteTemp
		effectHooksMu.Unlock()
	}
}

// LinkRowComponent is a reusable selectable row that opens a URL in the
// system browser when activated. displayValue is shown to the operator; url
// is what the browser will receive when the row is activated.
func LinkRowComponent(id, label, displayValue, url string) *SelectableRow {
	return NewSelectableRow(id, label, displayValue, "open \u21b5", func() {
		_ = OpenURL(url)
	})
}

// CopyPasteRowComponent is a reusable selectable row that copies text to the
// clipboard when activated and shows status in the value column.
func CopyPasteRowComponent(id, label, value, action string, doCopy func() CopyResult, onDone func(CopyResult)) *SelectableRow {
	return NewSelectableRow(id, label, value, action, func() {
		if onDone != nil {
			onDone(doCopy())
		} else {
			doCopy()
		}
	})
}

var _ tui.Component = (*SelectableRow)(nil)
