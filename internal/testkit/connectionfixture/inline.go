// Package connectionfixture constructs a current-schema Node document for
// tests of consumers outside the Connections package. Production code uses
// the same OpenInline and Node publisher without this fixture.
package connectionfixture

import (
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/nodefile"
	"github.com/swobuforge/swobu/internal/routing"
	"os"
	"path/filepath"
)

// Open creates a private current-schema Node fixture and opens its Connections.
func Open(path string, supports routing.ProviderSupport) (*connections.Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	config, err := configstore.OpenOrCreate(path)
	if err != nil {
		return nil, err
	}
	if err := config.Close(); err != nil {
		return nil, err
	}
	file, err := nodefile.Open(path)
	if err != nil {
		return nil, err
	}
	raw, err := file.ConnectionDocument()
	if err != nil {
		return nil, err
	}
	return connections.OpenInline(raw, file.PublishConnections, supports)
}
