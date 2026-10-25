package connections

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestDirectModelReferenceAdmissionAndDeletion(t *testing.T) {
	ctx := context.Background()
	registry, err := openInlineStoreFixture(t, filepath.Join(t.TempDir(), "swobu.yaml"), profile.SupportsSpec)
	if err != nil {
		t.Fatal(err)
	}
	routingDir := t.TempDir()
	if err := os.Chmod(routingDir, 0700); err != nil {
		t.Fatal(err)
	}
	workspaces, err := configstore.OpenOrCreate(filepath.Join(routingDir, "routing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer workspaces.Close()
	lifecycle, err := NewLifecycle(registry.Store, workspaces)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := connectionidentity.Parse("cn_shared")
	upstream, err := routing.NewStandardConnection("openai", "https://example.com/v1", "env:SYNTHETIC")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := NewModelSpec(upstream)
	if err != nil {
		t.Fatal(err)
	}
	value, err := New(id, "shared", spec, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Publish(value, Revision{}); err != nil {
		t.Fatal(err)
	}
	target, err := routing.FinalizeTarget(routing.TargetDraft{ID: "target", Model: "synthetic", Protocol: "responses", ConnectionID: id.String()}, profile.RoutingConstructionFacts())
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	route, _ := routing.ParseRouteName("main")
	config, err := (routing.Config{}).CreateWorkspace(routing.WorkspaceSeed{Slug: slug, Route: route, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspaces.Update(ctx, func(routing.Config) (routing.Config, error) { return config, nil }); err != nil {
		t.Fatal(err)
	}
	_, consumers, err := lifecycle.References(ctx, id)
	if err != nil || len(consumers) != 1 || consumers[0].Target != "target" {
		t.Fatal("direct consumer missing", err)
	}
	var inUse *InUseError
	if err := lifecycle.DeleteIfUnused(ctx, id, value.Revision()); !errors.As(err, &inUse) {
		t.Fatal("referenced Connection deletion admitted", err)
	}
	for _, tc := range []struct{ name, id, protocol string }{
		{"missing", "cn_missing", "responses"},
		{"incompatible", "cn_shared", "interactions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad, err := routing.FinalizeTarget(routing.TargetDraft{ID: "target", Model: "synthetic", Protocol: tc.protocol, ConnectionID: tc.id}, profile.RoutingConstructionFacts())
			if err != nil {
				t.Fatal(err)
			}
			candidate, err := (routing.Config{}).CreateWorkspace(routing.WorkspaceSeed{Slug: slug, Route: route, Target: bad})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := workspaces.Update(ctx, func(routing.Config) (routing.Config, error) { return candidate, nil }); err == nil {
				t.Fatal("invalid direct reference published")
			}
			got, err := workspaces.GetWorkspace(ctx, slug)
			if err != nil || got.Routes()[0].Tiers()[0].Targets()[0] != target {
				t.Fatal("rejection changed live target", err)
			}
		})
	}
}
