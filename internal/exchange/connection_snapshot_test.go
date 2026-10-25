package exchange

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/testkit/connectionfixture"
)

func TestModelConnectionProjectionFreezesRotationForSharedTargets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "swobu.yaml")
	store, err := connectionfixture.Open(path, profile.SupportsSpec)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := routing.ParseProvider("openai", profile.SupportsSpec)
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := routing.NewStandardConnection(provider, "", "env:BEFORE")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := connections.NewModelSpec(upstream)
	if err != nil {
		t.Fatal(err)
	}
	id, err := connectionidentity.New()
	if err != nil {
		t.Fatal(err)
	}
	value, err := connections.New(id, "Shared", spec, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(value, connections.Revision{}); err != nil {
		t.Fatal(err)
	}
	before, err := ProviderTargetFromModelConnection("first", value, "responses")
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := store.ReplaceCredentialBinding(t.Context(), id, value.Revision(), "", credentialref.Parse("env:BEFORE"), credentialref.Parse("env:AFTER"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, targetID := range []string{"first", "second"} {
		current, err := store.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		after, err := ProviderTargetFromModelConnection(targetID, current, "responses")
		if err != nil {
			t.Fatal(err)
		}
		got, revision, ok := after.ConnectionIdentity()
		if !ok || got != id || revision != 2 || after.CredentialRef != "env:AFTER" || after.TargetID != targetID {
			t.Fatal("shared rotation not reflected")
		}
		if after.TargetVersion != before.TargetVersion {
			t.Fatal("Connection rotation changed target version")
		}
		scope, err := after.ConnectionCredentialScope()
		if err != nil || scope.Kind() != credentialref.ScopeConnection || scope.Identifier() != id.String() {
			t.Fatal("credential authority drift")
		}
	}
	_, revision, ok := before.ConnectionIdentity()
	if !ok || revision != 1 || before.CredentialRef != "env:BEFORE" || rotated.Revision().String() != "2" {
		t.Fatal("rotation mutated earlier attempt")
	}
	if _, err := ProviderTargetFromModelConnection("invalid", connections.Connection{}, "responses"); err == nil {
		t.Fatal("empty registry snapshot accepted")
	}
	if _, err := ProviderTargetFromModelConnection("invalid", rotated, "unsupported-protocol"); err == nil {
		t.Fatal("unsupported provider protocol accepted")
	}
	endpoint, err := connections.ParseEndpoint("https://example.com/mcp")
	if err != nil {
		t.Fatal(err)
	}
	mcpSpec, err := connections.NewMCPHTTPSpec(endpoint, credentialref.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	mcp, err := connections.New(id, "Other kind", mcpSpec, value.CreatedAt())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ProviderTargetFromModelConnection("invalid", mcp, "responses"); err == nil {
		t.Fatal("MCP identity admitted as a Models provider")
	}
	reopened, err := connectionfixture.Open(path, profile.SupportsSpec)
	if err != nil {
		t.Fatal(err)
	}
	current, err := reopened.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	afterRestart, err := ProviderTargetFromModelConnection("first", current, "responses")
	if err != nil {
		t.Fatal(err)
	}
	_, restoredRevision, ok := afterRestart.ConnectionIdentity()
	if !ok || restoredRevision != 2 || afterRestart.CredentialRef != "env:AFTER" {
		t.Fatal("restart lost exact Connection binding")
	}
}
