package accessstate

import (
	"errors"
	"os"

	"github.com/swobuforge/swobu/internal/platform/atomicfile"
)

// openTestStore publishes the current Principal document to a disposable file
// for restart/fault tests. Production persists it only through swobu.yaml.

func openTestStore(path string) (*Store, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		raw = nil
	} else if err != nil {
		return nil, err
	}
	return OpenInline(raw, func(next []byte) error { return atomicfile.Replace(path, next, 0o600) })
}
