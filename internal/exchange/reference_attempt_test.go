package exchange

import (
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
	"testing"
	"time"
)

func TestAttemptFreezesReferencedConnectionRevision(t *testing.T) {
	id, _ := connectionidentity.Parse("cn_shared")
	upstream, err := routing.NewStandardConnection("openai", "https://example.com/v1", "env:SYNTHETIC")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := connections.NewModelSpec(upstream)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	current, err := connections.New(id, "shared", spec, at)
	if err != nil {
		t.Fatal(err)
	}
	target, err := routing.FinalizeTarget(routing.TargetDraft{ID: "target", Model: "synthetic", Protocol: "responses", ConnectionID: id.String()}, profile.RoutingConstructionFacts())
	if err != nil {
		t.Fatal(err)
	}
	target, err = routing.RestoreTarget(target.ID(), 7, target.Model(), target.Protocol(), target.ConnectionID())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	lookup := func(got connectionidentity.ID) (connections.Connection, error) {
		calls++
		if got != id {
			t.Fatal("wrong Connection lookup")
		}
		return current, nil
	}
	before, err := resolveProviderPath(target, lookup)
	if err != nil {
		t.Fatal(err)
	}
	rotatedUpstream, err := routing.NewStandardConnection("openai", "https://rotated.example/v1", "env:ROTATED")
	if err != nil {
		t.Fatal(err)
	}
	rotatedSpec, err := connections.NewModelSpec(rotatedUpstream)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := connections.ParseRevision("2")
	if err != nil {
		t.Fatal(err)
	}
	current, err = connections.Restore(id, "shared", rotatedSpec, revision, at, at.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	after, err := resolveProviderPath(target, lookup)
	if err != nil {
		t.Fatal(err)
	}
	beforeID, beforeRevision, bound := before.target.ConnectionIdentity()
	afterID, afterRevision, afterBound := after.target.ConnectionIdentity()
	if calls != 2 || !bound || !afterBound || beforeID != id || afterID != id || beforeRevision != 1 || afterRevision != 2 || before.target.BaseURL == after.target.BaseURL || before.target.CredentialRef == after.target.CredentialRef || before.target.TargetVersion != 7 || after.target.TargetVersion != 7 {
		t.Fatal("attempt snapshot or independent revisions lost")
	}
	if _, err := resolveProviderPath(target, nil); err == nil {
		t.Fatal("missing lookup admitted")
	}
	otherID, _ := connectionidentity.Parse("cn_other")
	other, err := connections.New(otherID, "other", spec, at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolveProviderPath(target, func(connectionidentity.ID) (connections.Connection, error) { return other, nil }); err == nil {
		t.Fatal("wrong Connection identity admitted")
	}
	incompatible, err := routing.FinalizeTarget(routing.TargetDraft{ID: "target", Model: "synthetic", Protocol: "interactions", ConnectionID: id.String()}, profile.RoutingConstructionFacts())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolveProviderPath(incompatible, lookup); err == nil {
		t.Fatal("incompatible protocol admitted")
	}
}
