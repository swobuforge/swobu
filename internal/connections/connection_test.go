package connections

import (
	"errors"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

func fixtureConnection(t *testing.T) Connection {
	t.Helper()
	id, err := connectionidentity.New()
	if err != nil {
		t.Fatal(err)
	}
	endpoint, _ := ParseEndpoint("https://example.com/mcp")
	spec, _ := NewMCPHTTPSpec(endpoint, credentialref.Parse("env:MCP_TOKEN"))
	value, err := New(id, "Shared MCP", spec, time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return value
}
func TestConnectionEditPreservesIdentityAndAdvancesRevision(t *testing.T) {
	original := fixtureConnection(t)
	changed, err := original.EditSameIdentity(original.Revision(), "Renamed", original.Spec(), original.UpdatedAt().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if changed.ID() != original.ID() || changed.CreatedAt() != original.CreatedAt() || changed.Revision().String() != "2" || original.Revision().String() != "1" || original.DisplayName() != "Shared MCP" {
		t.Fatal("identity/revision mutated incorrectly")
	}
	if _, err := changed.EditSameIdentity(original.Revision(), "Stale", original.Spec(), changed.UpdatedAt()); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale edit: %v", err)
	}
	endpoint, _ := ParseEndpoint("https://example.com/search")
	search, _ := NewSearchSpec("search", endpoint, credentialref.Ref{})
	if _, err := changed.EditSameIdentity(changed.Revision(), "Other kind", search, changed.UpdatedAt()); !errors.Is(err, ErrIdentityChange) {
		t.Fatalf("kind change: %v", err)
	}
	if _, err := changed.EditSameIdentity(changed.Revision(), "Past", changed.Spec(), original.CreatedAt()); err == nil {
		t.Fatal("accepted backward time")
	}
}
func TestConnectionRejectsInvalidSnapshotsAndZeroSpecs(t *testing.T) {
	c := fixtureConnection(t)
	for _, spec := range []Spec{nil, ModelSpec{}, MCPHTTPSpec{}, MCPStdioSpec{}, SearchSpec{}} {
		if _, err := New(c.ID(), "Example", spec, c.CreatedAt()); err == nil {
			t.Errorf("accepted %T", spec)
		}
	}
	for _, label := range []string{"", " padded", "bad\nname", string([]byte{0xff})} {
		if _, err := New(c.ID(), label, c.Spec(), c.CreatedAt()); err == nil {
			t.Errorf("accepted label %q", label)
		}
	}
	if _, err := Restore(c.ID(), c.DisplayName(), c.Spec(), c.Revision(), c.CreatedAt(), c.CreatedAt().Add(-time.Second)); err == nil {
		t.Fatal("accepted reversed timestamps")
	}
	for _, raw := range []string{"0", "01", "-1", "", "18446744073709551616"} {
		if _, err := ParseRevision(raw); err == nil {
			t.Errorf("accepted revision %q", raw)
		}
	}
	for _, raw := range []string{"", "Upper", "id/path", "id space"} {
		if _, err := connectionidentity.Parse(raw); err == nil {
			t.Errorf("accepted ID %q", raw)
		}
	}
}
