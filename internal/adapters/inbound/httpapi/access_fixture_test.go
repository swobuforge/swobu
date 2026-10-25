package httpapi

import (
	"errors"
	"os"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/platform/atomicfile"
)

// openTestAccessState uses a disposable file to exercise restarts and failed
// publications. Production Principal state is embedded in the Node document.
func openTestAccessState(path string) (*accessstate.Store, error) {
	if path == "" {
		return accessstate.OpenInline(nil, func([]byte) error { return nil })
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		raw = nil
	} else if err != nil {
		return nil, err
	}
	return accessstate.OpenInline(raw, func(next []byte) error {
		return atomicfile.Replace(path, next, 0o600)
	})
}
