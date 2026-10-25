package configstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

const orderedRoutesFixture = `schema_version: 5
workspaces:
  dev:
    routes:
      - pattern: coding
        tiers:
          - targets:
              - id: code
                version: 1
                model: gpt-5
                protocol: responses
                connection_id: cn_shared
      - pattern: "claude-*"
        tiers:
          - targets:
              - id: claude
                version: 1
                model: gpt-5
                protocol: responses
                connection_id: cn_shared
      - pattern: "deepseek/*"
        tiers:
          - targets:
              - id: slash
                version: 1
                model: gpt-5
                protocol: responses
                connection_id: cn_shared
      - pattern: "*"
        tiers:
          - targets:
              - id: fallback
                version: 1
                model: gpt-5
                protocol: responses
                connection_id: cn_shared
`

func namesForTest(routes []routing.Route) []string {
	names := make([]string, len(routes))
	for i, route := range routes {
		names[i] = route.Name().String()
	}
	return names
}

func TestRouteCodecPersistsOneOrderedSequenceAndRejectsExtraOrderingState(t *testing.T) {
	slug, _ := routing.ParseWorkspaceSlug("dev")
	parsed, err := decode([]byte(orderedRoutesFixture))
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := parsed.Workspace(slug)
	want := []string{"coding", "claude-*", "deepseek/*", "*"}
	if actual := namesForTest(ws.Routes()); !reflect.DeepEqual(actual, want) {
		t.Fatalf("initial priority: %v", actual)
	}
	serialized, err := encode(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(serialized, []byte("route_order:")) || !bytes.Contains(serialized, []byte("- pattern: coding")) {
		t.Fatalf("writer must emit only an ordered sequence: %s", serialized)
	}
	reopened, err := decode(serialized)
	if err != nil {
		t.Fatal(err)
	}
	ws, _ = reopened.Workspace(slug)
	if actual := namesForTest(ws.Routes()); !reflect.DeepEqual(actual, want) {
		t.Fatalf("priority after reopen: %v", actual)
	}
	for name, corrupt := range map[string]string{
		"duplicate pattern": strings.Replace(orderedRoutesFixture, "- pattern: \"claude-*\"", "- pattern: coding", 1),
		"missing pattern":   strings.Replace(orderedRoutesFixture, "- pattern: \"claude-*\"", "- tiers: []", 1),
		"invalid glob":      strings.Replace(orderedRoutesFixture, "- pattern: \"claude-*\"", "- pattern: '[unterminated'", 1),
		"parallel order":    strings.Replace(orderedRoutesFixture, "    routes:\n", "    route_order: [coding, '*']\n    routes:\n", 1),
		"old mapping":       strings.Replace(orderedRoutesFixture, "    routes:\n      - pattern: coding", "    routes:\n      coding:", 1),
		"old schema":        strings.Replace(orderedRoutesFixture, "schema_version: 5", "schema_version: 4", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decode([]byte(corrupt)); err == nil {
				t.Fatal("accepted invalid current routing representation")
			}
		})
	}
}

func TestSetRouteOrderCheckedPersistsAndHonorsConcurrency(t *testing.T) {
	store, path := openTestStore(t)
	if _, err := store.Update(context.Background(), addWorkspace(t, "dev")); err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("dev")
	wildcard, _ := routing.ParseRouteName("claude-*")
	if _, err := store.Update(context.Background(), func(current routing.Config) (routing.Config, error) {
		return current.CreateRoute(slug, wildcard, storeTarget(t, "family"))
	}); err != nil {
		t.Fatal(err)
	}
	previous, _ := store.Config().Workspace(slug)
	etag := fmt.Sprintf("%s-%d", previous.ManagementIncarnation(), previous.ManagementRevision())
	chat, _ := routing.ParseRouteName("chat")
	allowed := 0
	admit := func(context.Context) error { allowed++; return nil }
	committed, err := store.SetRouteOrderChecked(context.Background(), slug, []routing.RouteName{wildcard, chat}, etag, admit)
	if err != nil || allowed != 1 {
		t.Fatalf("reorder error=%v admission=%d", err, allowed)
	}
	if committed.ManagementRevision() <= previous.ManagementRevision() {
		t.Fatal("route priority revision did not advance")
	}
	if !reflect.DeepEqual(previous.TargetGenerations(), committed.TargetGenerations()) {
		t.Fatal("reorder changed target generations")
	}
	reopened, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	disk, _ := reopened.Config().Workspace(slug)
	if actual := namesForTest(disk.Routes()); !reflect.DeepEqual(actual, []string{"claude-*", "chat"}) {
		t.Fatalf("reopened order %v", actual)
	}
	saved, _ := os.ReadFile(path)
	if _, err := store.SetRouteOrderChecked(context.Background(), slug, []routing.RouteName{chat, wildcard}, etag, admit); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale write: %v", err)
	}
	if allowed != 1 {
		t.Fatal("stale write reached admission callback")
	}
	nextETag := fmt.Sprintf("%s-%d", committed.ManagementIncarnation(), committed.ManagementRevision())
	if _, err := store.SetRouteOrderChecked(context.Background(), slug, []routing.RouteName{chat, wildcard}, nextETag, func(context.Context) error { return errors.New("denied") }); err == nil {
		t.Fatal("unadmitted reorder")
	}
	noOp, err := store.SetRouteOrderChecked(context.Background(), slug, []routing.RouteName{wildcard, chat}, nextETag, admit)
	if err != nil || noOp.ManagementRevision() != committed.ManagementRevision() {
		t.Fatalf("no-op changed revision: %v", err)
	}
	unchanged, _ := os.ReadFile(path)
	if !bytes.Equal(saved, unchanged) {
		t.Fatal("rejected/no-op write modified the durable config")
	}
}
