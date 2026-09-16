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

// CopyToClipboard attempts only clipboard transport. Callers that may persist
// their payload must opt into SaveTextFile separately.
func CopyToClipboard(text string) ClipboardResult {
	_, write, _ := currentEffectHooks()
	return write(text)
}

// SaveTextFile explicitly persists text to a temporary file. The caller owns
// the payload policy and supplies a truthful semantic filename prefix.
func SaveTextFile(prefix, text string) (string, error) {
	_, _, writeTemp := currentEffectHooks()
	return writeTemp("", prefix, text)
}

// ClipboardResult describes only the outcome of a clipboard transfer.
type ClipboardResult struct {
	Status CopyStatus
	Err    error // valid when Status == CopyFailed
}

// CopyStatus indicates which path the copy operation took.
type CopyStatus int

const (
	CopyOK          CopyStatus = iota // written to system clipboard
	CopyUnavailable                   // clipboard integration is unavailable
	CopyFailed                        // clipboard transport failed
)

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

func defaultClipboardWrite(string) ClipboardResult {
	return ClipboardResult{Status: CopyFailed, Err: fmt.Errorf("clipboard write not wired")}
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
	func(string) ClipboardResult,
	func(dir, prefix, text string) (string, error),
) {
	effectHooksMu.RLock()
	defer effectHooksMu.RUnlock()
	return browserOpen, clipboardWrite, clipboardWriteTemp
}

// RegisterEffectHooks wires platform-specific implementations for the package.
// The adapter calls this once at process startup. Tests may defer the returned
// cleanup to restore the previous hooks and avoid order-dependent global state.
func RegisterEffectHooks(open func(string) error, write func(string) ClipboardResult, writeTemp func(dir, prefix, text string) (string, error)) func() {
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
func LinkRowComponent(id, label, displayValue, url string, onDone func(error)) *SelectableRow {
	return NewSelectableRow(id, label, displayValue, "open \u21b5", func() {
		err := OpenURL(url)
		if onDone != nil {
			onDone(err)
		}
	})
}

// CopyPasteRowComponent is a reusable selectable row that copies text to the
// clipboard when activated and shows status in the value column.
func CopyPasteRowComponent(id, label, value, action string, doCopy func() ClipboardResult, onDone func(ClipboardResult)) *SelectableRow {
	return NewSelectableRow(id, label, value, action, func() {
		if onDone != nil {
			onDone(doCopy())
		} else {
			doCopy()
		}
	})
}

var _ tui.Component = (*SelectableRow)(nil)
