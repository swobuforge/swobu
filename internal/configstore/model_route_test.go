package configstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestPutModelRouteCheckedOneRevisionNoExtraState(t *testing.T) {
	store, path := openTestStore(t)
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	original, _ := store.GetWorkspace(t.Context(), slug)
	name := original.Routes()[0].Name()
	route, _ := original.Route(name)
	spec := route.Spec()
	expected := fmt.Sprintf("%s-%d", original.ManagementIncarnation(), original.ManagementRevision())
	admit := func(context.Context) error { return nil }
	before, _ := os.ReadFile(path)
	denied := errors.New("denied")
	if _, err := store.PutModelRouteChecked(t.Context(), slug, name, spec, expected, func(context.Context) error { return denied }); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	if _, err := store.PutModelRouteChecked(t.Context(), slug, name, spec, "stale", admit); !errors.Is(err, ErrStaleRevision) {
		t.Fatal(err)
	}
	if _, err := store.PutModelRouteChecked(t.Context(), slug, name, spec, expected, nil); err == nil {
		t.Fatal("missing admission")
	}
	if _, err := store.PutModelRouteChecked(t.Context(), slug, name, routing.RouteSpec{}, expected, admit); err == nil {
		t.Fatal("invalid topology")
	}
	noOp, err := store.PutModelRouteChecked(t.Context(), slug, name, spec, expected, admit)
	after, _ := os.ReadFile(path)
	if err != nil || noOp.ManagementRevision() != original.ManagementRevision() || !bytes.Equal(before, after) {
		t.Fatal("no-op wrote a receipt", err)
	}
	spec.Tiers[0].Targets[0].Settings.Model, _ = routing.ParseUpstreamModel("changed")
	oldRename := store.ops.rename
	store.ops.rename = func(string, string) error { return errors.New("disk failure") }
	if _, err := store.PutModelRouteChecked(t.Context(), slug, name, spec, expected, admit); err == nil {
		t.Fatal("disk failure swallowed")
	}
	after, _ = os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed commit changed disk")
	}
	stillCurrent, err := store.GetWorkspace(t.Context(), slug)
	if err != nil || !reflect.DeepEqual(original, stillCurrent) {
		t.Fatal("failed publication changed snapshot", err)
	}
	store.ops.rename = oldRename
	committed, err := store.PutModelRouteChecked(t.Context(), slug, name, spec, expected, admit)
	if err != nil || committed.ManagementRevision() != original.ManagementRevision()+1 {
		t.Fatal("commit", err)
	}
	amended, _ := committed.Route(name)
	if amended.Tiers()[0].Targets()[0].Version() != route.Tiers()[0].Targets()[0].Version()+1 {
		t.Fatal("target identity version not advanced")
	}
	if _, err := store.PutModelRouteChecked(t.Context(), slug, name, spec, expected, admit); !errors.Is(err, ErrStaleRevision) {
		t.Fatal("stale ETag accepted", err)
	}
	raw, _ := os.ReadFile(path)
	if bytes.Contains(raw, []byte("route_replacement_receipts:")) || bytes.Contains(raw, []byte("route_replacement_audits:")) {
		t.Fatal("parallel journal survived")
	}
}

func TestCompetingModelRouteWritesAcceptOneRevision(t *testing.T) {
	store, _ := openTestStore(t)
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	original, _ := store.GetWorkspace(t.Context(), slug)
	route := original.Routes()[0]
	expected := fmt.Sprintf("%s-%d", original.ManagementIncarnation(), original.ManagementRevision())
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, model := range []string{"first", "second"} {
		wg.Add(1)
		go func(model string) {
			defer wg.Done()
			spec := route.Spec()
			spec.Tiers[0].Targets[0].Settings.Model, _ = routing.ParseUpstreamModel(model)
			_, err := store.PutModelRouteChecked(context.Background(), slug, route.Name(), spec, expected, func(context.Context) error { return nil })
			results <- err
		}(model)
	}
	wg.Wait()
	close(results)
	accepted, stale := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if errors.Is(err, ErrStaleRevision) {
			stale++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || stale != 1 {
		t.Fatalf("accepted=%d stale=%d", accepted, stale)
	}
}

func TestRouteWriteNeverReusesDeletedWorkspaceLifetime(t *testing.T) {
	store, path := openTestStore(t)
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	previous, _ := store.GetWorkspace(t.Context(), slug)
	name := previous.Routes()[0].Name()
	route, _ := previous.Route(name)
	expected := fmt.Sprintf("%s-%d", previous.ManagementIncarnation(), previous.ManagementRevision())
	ctx, cancel := context.WithCancel(t.Context())
	if _, err := store.PutModelRouteChecked(ctx, slug, name, route.Spec(), expected, func(context.Context) error { cancel(); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled apply")
	}
	if _, err := store.Update(t.Context(), func(c routing.Config) (routing.Config, error) { return c.DeleteWorkspace(slug) }); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if _, err := store.PutModelRouteChecked(t.Context(), slug, name, route.Spec(), expected, func(context.Context) error { return nil }); !errors.Is(err, ErrStaleRevision) {
		t.Fatal("old lifetime")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("old lifetime wrote disk")
	}
}

// Route topology and priority are edits of the same Workspace, so either write
// invalidates the other writer's review even when its own route is untouched.
func TestModelRouteAndPriorityShareWorkspaceRevision(t *testing.T) {
	store, _ := openTestStore(t)
	if _, err := store.Update(t.Context(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	wildcard, _ := routing.ParseRouteName("*")
	if _, err := store.Update(t.Context(), func(c routing.Config) (routing.Config, error) {
		return c.CreateRoute(slug, wildcard, storeTarget(t, "family"))
	}); err != nil {
		t.Fatal(err)
	}
	original, _ := store.GetWorkspace(t.Context(), slug)
	route := original.Routes()[0]
	etag := func(w routing.Workspace) string {
		return fmt.Sprintf("%s-%d", w.ManagementIncarnation(), w.ManagementRevision())
	}
	admit := func(context.Context) error { return nil }
	reordered, err := store.SetRouteOrderChecked(t.Context(), slug, []routing.RouteName{wildcard, route.Name()}, etag(original), admit)
	if err != nil {
		t.Fatal(err)
	}
	spec := route.Spec()
	spec.Tiers[0].Targets[0].Settings.Model, _ = routing.ParseUpstreamModel("changed")
	if _, err := store.PutModelRouteChecked(t.Context(), slug, route.Name(), spec, etag(original), admit); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("priority change did not invalidate topology review: %v", err)
	}
	committed, err := store.PutModelRouteChecked(t.Context(), slug, route.Name(), spec, etag(reordered), admit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetRouteOrderChecked(t.Context(), slug, []routing.RouteName{route.Name(), wildcard}, etag(reordered), admit); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("topology change did not invalidate priority review: %v", err)
	}
	if got := namesForTest(committed.Routes()); len(got) != 2 || got[0] != "*" {
		t.Fatalf("topology edit changed priority: %v", got)
	}
}
