package bootstrap

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

// Current schema fixtures shared by daemon ingress and provider proofs. Keep
// these independent of any particular administrative interface or test suite.
func writeCurrentInferenceFixture(t *testing.T, path, root, providerURL, protocol string, withMCP bool) {
	writeCurrentWorkspaceFixture(t, path, root, "research", providerURL, protocol, withMCP)
}

func writeCurrentWorkspaceFixture(t *testing.T, path, root, slug, providerURL, protocol string, withMCP bool) {
	t.Helper()
	id, err := connectionidentity.Parse("cn_admin")
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := routing.NewCustomConnection("custom", providerURL+"/v1", nil)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := connections.NewModelSpec(upstream)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := connections.New(id, "Admin fixture", spec, time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	mcp := ""
	if withMCP {
		mcp = "    mcp_mounts:\n      docs:\n        connection_id: cn_fixture_docs\n        allowed_tools: [search]\n"
	}
	fixture := fmt.Sprintf("schema_version: 5\nworkspaces:\n  %s:\n%s    routes:\n      - pattern: chat\n        tiers:\n          - targets:\n              - id: primary\n                version: 1\n                model: fixture\n                protocol: %s\n                connection_id: cn_admin\n", slug, mcp, protocol)
	if err := os.WriteFile(path, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	publishCurrentConnectionFixtures(t, path, stored)
}

type currentFixtureWorkspace struct {
	slug string
	url  string
}

func writeCurrentMultiWorkspaceFixture(t *testing.T, path, root, protocol string, values ...currentFixtureWorkspace) {
	t.Helper()
	var document strings.Builder
	document.WriteString("schema_version: 5\nworkspaces:\n")
	registry := make([]connections.Connection, 0, len(values))
	for _, value := range values {
		id, err := connectionidentity.Parse("cn_" + value.slug)
		if err != nil {
			t.Fatal(err)
		}
		upstream, err := routing.NewCustomConnection("custom", value.url+"/v1", nil)
		if err != nil {
			t.Fatal(err)
		}
		spec, err := connections.NewModelSpec(upstream)
		if err != nil {
			t.Fatal(err)
		}
		stored, err := connections.New(id, value.slug+" fixture", spec, time.Unix(0, 0).UTC())
		if err != nil {
			t.Fatal(err)
		}
		registry = append(registry, stored)
		fmt.Fprintf(&document, "  %s:\n    routes:\n      - pattern: chat\n        tiers:\n          - targets:\n              - id: primary\n                version: 1\n                model: fixture\n                protocol: %s\n                connection_id: cn_%s\n", value.slug, protocol, value.slug)
	}
	if err := os.WriteFile(path, []byte(document.String()), 0600); err != nil {
		t.Fatal(err)
	}
	publishCurrentConnectionFixtures(t, path, registry...)
}

func writeCurrentTwoRouteFixture(t *testing.T, path, root, slug, primaryURL, secondaryURL string) {
	t.Helper()
	values := []currentFixtureWorkspace{{slug: "primary", url: primaryURL}, {slug: "secondary", url: secondaryURL}}
	registry := make([]connections.Connection, 0, 2)
	for _, value := range values {
		id, _ := connectionidentity.Parse("cn_" + value.slug)
		upstream, err := routing.NewCustomConnection("custom", value.url+"/v1", nil)
		if err != nil {
			t.Fatal(err)
		}
		spec, err := connections.NewModelSpec(upstream)
		if err != nil {
			t.Fatal(err)
		}
		stored, err := connections.New(id, value.slug+" fixture", spec, time.Unix(0, 0).UTC())
		if err != nil {
			t.Fatal(err)
		}
		registry = append(registry, stored)
	}
	fixture := fmt.Sprintf("schema_version: 5\nworkspaces:\n  %s:\n    routes:\n      - pattern: chat\n        tiers:\n          - targets:\n              - id: primary\n                version: 1\n                model: fixture\n                protocol: responses\n                connection_id: cn_primary\n      - pattern: alternate\n        tiers:\n          - targets:\n              - id: secondary\n                version: 1\n                model: secondary-model\n                protocol: responses\n                connection_id: cn_secondary\n", slug)
	if err := os.WriteFile(path, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	publishCurrentConnectionFixtures(t, path, registry...)
}
