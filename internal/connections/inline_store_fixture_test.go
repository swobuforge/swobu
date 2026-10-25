package connections

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/swobuforge/swobu/internal/nodefile"
	"github.com/swobuforge/swobu/internal/routing"
)

// inlineStoreFixture exercises the real Node YAML publication boundary. It has
// no private registry file; injected failures replace the Node section writer.
type inlineStoreFixture struct {
	*Store
	path           string
	publishSection func([]byte) error
}

func openInlineStoreFixture(t *testing.T, path string, supports routing.ProviderSupport) (*inlineStoreFixture, error) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	node, err := nodefile.Open(path)
	if err != nil {
		return nil, err
	}
	raw, err := node.ConnectionDocument()
	if err != nil {
		return nil, err
	}
	f := &inlineStoreFixture{path: path, publishSection: node.PublishConnections}
	f.Store, err = OpenInline(raw, func(desired []byte) error { return f.publishSection(desired) }, supports)
	return f, err
}
