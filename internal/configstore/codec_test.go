package configstore

import (
	"bytes"
	"fmt"
	"github.com/swobuforge/swobu/internal/routing"
	"strings"
	"testing"
)

const versionedTargetYAML = `schema_version: 5
workspaces:
  dev:
    routes:
      - pattern: chat
        tiers:
          - targets:
              - id: x
                version: 1
                model: gpt-5
                protocol: responses
                connection_id: cn_shared
              - id: keep
                version: 1
                model: gpt-5
                protocol: responses
                connection_id: cn_shared
`

func TestCodecAcceptsEmptyInstallation(t *testing.T) {
	config, err := decode([]byte("schema_version: 5\nworkspaces: {}\n"))
	if err != nil || config.WorkspaceCount() != 0 {
		t.Fatalf("config = %#v, error = %v", config, err)
	}
}

func TestCodecRoundTripsEmptyWorkspace(t *testing.T) {
	raw := []byte("schema_version: 5\nworkspaces:\n  research:\n    routes: []\n")
	config, err := decode(raw)
	if err != nil {
		t.Fatalf("decode empty workspace: %v", err)
	}
	slug, _ := routing.ParseWorkspaceSlug("research")
	workspace, ok := config.Workspace(slug)
	if !ok || len(workspace.Routes()) != 0 {
		t.Fatalf("workspace=%v ok=%v", workspace, ok)
	}
	encoded, err := encode(config)
	if err != nil || !strings.Contains(string(encoded), "routes: []") {
		t.Fatalf("encoded=%s err=%v", encoded, err)
	}
}

func TestReferenceSchemaRoundTripsExplicitMCPMounts(t *testing.T) {
	raw := []byte(`schema_version: 5
workspaces:
  research:
    routes: []
    mcp_mounts:
      docs:
        connection_id: docs-connector
        allowed_tools: [search, fetch]
`)
	config, err := decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	slug, err := routing.ParseWorkspaceSlug("research")
	if err != nil {
		t.Fatal(err)
	}
	workspace, ok := config.Workspace(slug)
	if !ok {
		t.Fatal("workspace missing")
	}
	mounts := workspace.MCPMounts()
	if len(mounts) != 1 || mounts["docs"].ConnectionID != "docs-connector" {
		t.Fatalf("mounts=%#v", mounts)
	}
	encoded, err := encodeDocument(config, routing.ReferenceSchemaVersion)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte("connection_id: docs-connector")) || !bytes.Contains(encoded, []byte("allowed_tools:")) {
		t.Fatalf("encoded=%s", encoded)
	}
}

func TestReferenceSchemaRoundTripsSearchConnection(t *testing.T) {
	raw := []byte(`schema_version: 5
workspaces:
  research:
    routes: []
    search:
      connection_id: local-search
`)
	config, err := decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("research")
	workspace, ok := config.Workspace(slug)
	if !ok || workspace.SearchConnectionID() != "local-search" {
		t.Fatalf("workspace=%#v ok=%v", workspace, ok)
	}
	encoded, err := encodeDocument(config, routing.ReferenceSchemaVersion)
	if err != nil || !bytes.Contains(encoded, []byte("connection_id: local-search")) {
		t.Fatalf("encoded=%s err=%v", encoded, err)
	}
}

// The v5 layout has one reference spelling. Old documents must not be
// interpreted as empty MCP/Search configuration after an upgrade.
func TestSourceEraMountAndSearchYAMLRejected(t *testing.T) {
	cases := map[string]string{
		"old document version": `schema_version: 4
workspaces: {}`,
		"old mount collection": `schema_version: 5
workspaces:
  research:
    routes: []
    mcp_sources: {docs: {source_id: docs}}`,
		"old mount field": `schema_version: 5
workspaces:
  research:
    routes: []
    mcp_mounts: {docs: {source_id: docs}}`,
		"old Search field": `schema_version: 5
workspaces:
  research:
    routes: []
    search: {source_id: web}`,
		"invalid mount Connection": `schema_version: 5
workspaces:
  research:
    routes: []
    mcp_mounts: {docs: {connection_id: ../secret}}`,
		"invalid Search Connection": `schema_version: 5
workspaces:
  research:
    routes: []
    search: {connection_id: Web Search}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decode([]byte(raw)); err == nil {
				t.Fatal("obsolete or invalid source representation admitted")
			}
		})
	}
}

func TestCodecRestoresPositiveTargetVersionAndRequiresMatchingLedger(t *testing.T) {
	versioned := strings.Replace(versionedTargetYAML, "id: x\n                version: 1", "id: x\n                version: 7", 1)
	config, err := decode([]byte(versioned))
	if err != nil {
		t.Fatalf("decode positive version: %v", err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	workspace, _ := config.Workspace(slug)
	id, _ := routing.ParseTargetID("x")
	if got := workspace.TargetGenerations()[id]; got != 7 {
		t.Fatalf("restored generation = %d, want 7", got)
	}

	for _, generation := range []uint64{6, 8} {
		withLedger := strings.Replace(versioned, "    routes:\n", "    target_generations: {x: "+fmt.Sprint(generation)+"}\n    routes:\n", 1)
		if _, err := decode([]byte(withLedger)); err == nil || !strings.Contains(err.Error(), "must equal the active target version") {
			t.Fatalf("mismatched ledger %d error = %v", generation, err)
		}
	}
}

func TestCodecRejectsInvalidGenerationLedgerEntries(t *testing.T) {
	for name, ledger := range map[string]string{
		"zero":       "{x: 0}",
		"invalid ID": "{'bad/id': 1}",
	} {
		t.Run(name, func(t *testing.T) {
			raw := strings.Replace(versionedTargetYAML, "    routes:\n", "    target_generations: "+ledger+"\n    routes:\n", 1)
			if _, err := decode([]byte(raw)); err == nil {
				t.Fatal("invalid generation ledger unexpectedly decoded")
			}
		})
	}
}

func TestCodecPersistsAbsentTargetGenerationAcrossRestart(t *testing.T) {
	config, err := decode([]byte(versionedTargetYAML))
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	routeName, _ := routing.ParseRouteName("chat")
	workspace, _ := config.Workspace(slug)
	route, _ := workspace.Route(routeName)
	original := route.Spec()
	keepOnly := routing.RouteSpec{Tiers: []routing.TierSpec{{Targets: []routing.TargetSpec{original.Tiers[0].Targets[1]}}}}
	withoutX, err := config.ApplyRouteSpec(slug, routeName, keepOnly)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := encode(withoutX)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "x: 1") {
		t.Fatalf("tombstoned generation missing from persistence:\n%s", raw)
	}
	restarted, err := decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	readd := keepOnly
	readd.Tiers = append(readd.Tiers, routing.TierSpec{Targets: []routing.TargetSpec{original.Tiers[0].Targets[0]}})
	next, err := restarted.ApplyRouteSpec(slug, routeName, readd)
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ = next.Workspace(slug)
	route, _ = workspace.Route(routeName)
	for _, tier := range route.Tiers() {
		for _, target := range tier.Targets() {
			if target.ID().String() == "x" && target.Version() != 2 {
				t.Fatalf("re-added generation after restart = %d, want 2", target.Version())
			}
		}
	}
}

func TestCodecRejectsUnknownDuplicateAndLegacyFields(t *testing.T) {
	tests := map[string]string{
		"unknown root":   "schema_version: 5\nworkspaces: {}\nunknown: true\n",
		"startup field":  "schema_version: 5\nserver: {listen: '127.0.0.1:7926'}\nworkspaces: {}\n",
		"unknown nested": strings.Replace(versionedTargetYAML, "connection_id: cn_shared", "connection_id: cn_shared\n                unknown: true", 1),
		"duplicate":      "schema_version: 5\nschema_version: 5\nworkspaces: {}\n",
		"legacy":         "bind_addr: 127.0.0.1:7926\nendpoints: []\n",
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := decode([]byte(raw)); err == nil {
				t.Fatal("decode unexpectedly succeeded")
			}
		})
	}
}

func TestDecodeUnknownFieldsDoNotLeakInternalTypeName(t *testing.T) {
	_, err := decode([]byte("bind_addr: 127.0.0.1:7926\nendpoints: []\n"))
	if err == nil {
		t.Fatal("decode unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), "documentDTO") {
		t.Fatalf("decode error leaks internal type name: %v", err)
	}
	if !strings.Contains(err.Error(), "bind_addr") || !strings.Contains(err.Error(), "endpoints") {
		t.Fatalf("decode error does not name the offending fields: %v", err)
	}
}

func TestCodecRejectsDefaultRouteSelector(t *testing.T) {
	const raw = "schema_version: 5\nworkspaces:\n  research:\n    routes: []\n    default_route: coding\n"
	if _, err := decode([]byte(raw)); err == nil {
		t.Fatal("retired default_route field must not create a second route selector")
	}
}

// MCP mount edits are ordinary Workspace transactions. A retired mount replay
// journal cannot be silently loaded or rewritten as current Node state.
func TestCodecRejectsRetiredMCPMountReceiptJournal(t *testing.T) {
	raw := []byte("schema_version: 5\nworkspaces: {}\nmcp_mount_receipts: {}\n")
	if _, err := decode(raw); err == nil {
		t.Fatal("retired MCP mount replay journal was admitted")
	}
}
