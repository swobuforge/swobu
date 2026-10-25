package configstore

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestSearchFallbackCodecPersistsOrderAndPreservesService(t *testing.T) {
	raw := []byte(`schema_version: 5
workspaces:
  research:
    routes: []
    enabled_services: [search]
    search:
      connection_id: cn_primary
      fallback_connection_ids: [cn_backup, cn_last]
`)
	got, err := decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("research")
	before, ok := got.Workspace(slug)
	if !ok || !reflect.DeepEqual(before.SearchConnections(), []string{"cn_primary", "cn_backup", "cn_last"}) {
		t.Fatalf("decoded Search order=%v", before.SearchConnections())
	}
	encoded, err := encode(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte("fallback_connection_ids:")) {
		t.Fatalf("fallbacks absent from persisted document:\n%s", encoded)
	}
	reloaded, err := decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	after, ok := reloaded.Workspace(slug)
	if !ok || !reflect.DeepEqual(after.SearchConnections(), before.SearchConnections()) || !after.HasService(routing.ServiceSearch) {
		t.Fatalf("reload changed Search route/services: %v", after.SearchConnections())
	}
	if len(after.Routes()) != 0 || len(after.MCPMounts()) != 0 {
		t.Fatal("route serialization invented unrelated services")
	}
}
