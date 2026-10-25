package connections

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/configstore"
	domain "github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/testkit/connectionfixture"
)

func TestQueriesFilterOnlyPublicFieldsAndReturnRevisionBoundReferences(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	registry, err := connectionfixture.Open(filepath.Join(dir, "swobu.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	workspaces, err := configstore.OpenOrCreate(filepath.Join(dir, "routing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer workspaces.Close()
	provider, _ := routing.ParseProvider("fixture", func(string) bool { return true })
	upstream, err := routing.NewStandardConnection(provider, "https://example.com/api?token=hidden-query-token", "env:HIDDEN_CREDENTIAL")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := domain.NewModelSpec(upstream)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := connectionidentity.New()
	value, err := domain.New(id, "Shared Account", spec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Publish(value, domain.Revision{}); err != nil {
		t.Fatal(err)
	}
	lifecycle, err := domain.NewLifecycle(registry, workspaces)
	if err != nil {
		t.Fatal(err)
	}
	queries, err := NewQueries(registry, lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		query string
		count int
	}{{"shared", 1}, {" EXAMPLE.COM ", 1}, {"hidden-query-token", 0}, {"HIDDEN_CREDENTIAL", 0}, {"absent", 0}, {"", 1}} {
		got, err := queries.List(context.Background(), Filter{Query: test.query})
		if err != nil || len(got) != test.count {
			t.Fatalf("query %q: count=%d error=%v", test.query, len(got), err)
		}
	}
	if got, err := queries.List(context.Background(), Filter{Kind: domain.MCP}); err != nil || len(got) != 0 {
		t.Fatal("kind filter ignored")
	}
	if _, err := queries.List(context.Background(), Filter{Kind: "unknown"}); err == nil {
		t.Fatal("invalid kind accepted")
	}
	check, err := domain.NewCheck(value, value.UpdatedAt(), domain.Reachable, domain.AuthenticationUnknown, nil, domain.CheckOK)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Observe(check); err != nil {
		t.Fatal(err)
	}
	got, err := queries.Get(context.Background(), id)
	if err != nil || got.LatestCheck == nil || got.LatestCheck.ConnectionRevision != got.Revision {
		t.Fatal("detail lost coherent health")
	}
	references, err := queries.References(context.Background(), id)
	if err != nil || references.ConnectionID != id.String() || references.ConnectionRevision != value.Revision().String() || references.Consumers == nil || len(references.Consumers) != 0 {
		t.Fatal("reference query lost identity/empty array")
	}
	missing, _ := connectionidentity.New()
	if _, err := queries.Get(context.Background(), missing); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing detail: %v", err)
	}
	if _, err := queries.References(context.Background(), missing); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing references: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := queries.List(ctx, Filter{}); !errors.Is(err, context.Canceled) {
		t.Fatal("list ignored cancellation")
	}
	if _, err := queries.Get(ctx, id); !errors.Is(err, context.Canceled) {
		t.Fatal("get ignored cancellation")
	}
	if _, err := queries.References(ctx, id); !errors.Is(err, context.Canceled) {
		t.Fatal("references ignored cancellation")
	}
}
