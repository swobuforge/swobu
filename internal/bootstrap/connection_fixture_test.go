package bootstrap

import (
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/nodefile"
	"github.com/swobuforge/swobu/internal/profile"
)

// publishMCPFixtureConnection creates the durable identity that an injected
// test binding stands in for. The binding remains deterministic and local, but
// workspace references and lifecycle validation exercise the Connection path.
func publishMCPFixtureConnection(t *testing.T, configPath, rawID string) {
	t.Helper()
	store, err := openInlineFixtureRegistry(t, configPath)
	if err != nil {
		t.Fatal(err)
	}
	id, err := connectionidentity.Parse(rawID)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := connections.ParseEndpoint("https://fixture.example.test/mcp")
	if err != nil {
		t.Fatal(err)
	}
	// This fixture has no live transport. Missing credentials keep bootstrap
	// from contacting the deliberately fictional endpoint; transport journeys
	// inject a revision-bound binding or publish their own real test server.
	t.Setenv("SWOBU_MISSING_MCP_FIXTURE", "")
	spec, err := connections.NewMCPHTTPSpec(endpoint, credentialref.Parse("env:SWOBU_MISSING_MCP_FIXTURE"))
	if err != nil {
		t.Fatal(err)
	}
	spec, err = spec.WithOperatorDeclarations("fixture account", "fixture tool access")
	if err != nil {
		t.Fatal(err)
	}
	value, err := connections.New(id, "Fixture "+rawID, spec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(value, connections.Revision{}); err != nil {
		t.Fatal(err)
	}
}

func publishSearchFixtureConnection(t *testing.T, configPath, rawID string) {
	t.Helper()
	store, err := openInlineFixtureRegistry(t, configPath)
	if err != nil {
		t.Fatal(err)
	}
	id, err := connectionidentity.Parse(rawID)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := connections.ParseEndpoint("https://fixture.example.test/search")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := connections.NewSearchSpec("fixture", endpoint, credentialref.Parse("env:SWOBU_MISSING_SEARCH_FIXTURE"))
	if err != nil {
		t.Fatal(err)
	}
	value, err := connections.New(id, "Fixture "+rawID, spec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(value, connections.Revision{}); err != nil {
		t.Fatal(err)
	}
}

// publishCurrentConnectionFixtures exercises ordinary Connection publication,
// not a retired upgrade/migration encoder, when setting up daemon tests.
func publishCurrentConnectionFixtures(t *testing.T, configPath string, values ...connections.Connection) {
	t.Helper()
	store, err := openInlineFixtureRegistry(t, configPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range values {
		if err := store.Publish(value, connections.Revision{}); err != nil {
			t.Fatal(err)
		}
	}
}

// openInlineFixtureRegistry intentionally exercises the final one-document
// authority instead of silently teaching tests to rely on a sidecar.
func openInlineFixtureRegistry(t *testing.T, configPath string) (*connections.Store, error) {
	t.Helper()
	managed, err := configstore.OpenOrCreate(configPath)
	if err != nil {
		return nil, err
	}
	if err := managed.Close(); err != nil {
		return nil, err
	}
	doc, err := nodefile.Open(configPath)
	if err != nil {
		return nil, err
	}
	raw, err := doc.ConnectionDocument()
	if err != nil {
		return nil, err
	}
	return connections.OpenInline(raw, doc.PublishConnections, profile.SupportsSpec)
}
