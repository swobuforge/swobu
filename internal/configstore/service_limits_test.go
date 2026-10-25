package configstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/swobuforge/swobu/internal/routing"
)

func TestWorkspaceServiceLimitsAreDurableAndETagChecked(t *testing.T) {
	store, path := openTestStore(t)
	slug, _ := routing.ParseWorkspaceSlug("engineering")
	if _, err := store.Update(t.Context(), func(c routing.Config) (routing.Config, error) {
		return c.CreateEmptyWorkspace(routing.EmptyWorkspaceSeed{Slug: slug})
	}); err != nil {
		t.Fatal(err)
	}
	before, _ := store.Config().Workspace(slug)
	etag := fmt.Sprintf("%s-%d", before.ManagementIncarnation(), before.ManagementRevision())
	policy := routing.ServiceLimits{Models: routing.ServiceQuota{RequestsPerMinute: 12, Concurrent: 3}, MCP: routing.ServiceQuota{RequestsPerMinute: 4}, Search: routing.ServiceQuota{RequestsPerMinute: 7}}
	admitted := 0
	authorize := func(context.Context) error { admitted++; return nil }
	committed, err := store.SetServiceLimitsChecked(t.Context(), slug, policy, etag, authorize)
	if err != nil || admitted != 1 {
		t.Fatalf("commit=%v calls=%d", err, admitted)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(raw, []byte("service_limits:")) {
		t.Fatalf("not saved %s %v", raw, err)
	}
	if _, err := store.SetServiceLimitsChecked(t.Context(), slug, routing.ServiceLimits{}, etag, authorize); !errors.Is(err, ErrStaleRevision) {
		t.Fatal("stale mutation accepted", err)
	}
	currentETag := fmt.Sprintf("%s-%d", committed.ManagementIncarnation(), committed.ManagementRevision())
	if _, err := store.SetServiceLimitsChecked(t.Context(), slug, routing.ServiceLimits{}, currentETag, func(context.Context) error { return errors.New("withdrawn") }); err == nil {
		t.Fatal("revoked admission accepted")
	}
	persisted, _ := os.ReadFile(path)
	if !bytes.Equal(persisted, raw) {
		t.Fatal("failed mutation wrote state")
	}
	same, err := store.SetServiceLimitsChecked(t.Context(), slug, policy, currentETag, authorize)
	if err != nil || same.ManagementRevision() != committed.ManagementRevision() {
		t.Fatal("no-op advanced Workspace revision", err)
	}
	persisted, _ = os.ReadFile(path)
	if !bytes.Equal(persisted, raw) {
		t.Fatal("no-op rewrote Node file")
	}
	reopened, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restored, _ := reopened.Config().Workspace(slug)
	if _, err := reopened.SetServiceLimitsChecked(t.Context(), slug, routing.ServiceLimits{}, currentETag, authorize); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("read-only mutation accepted: %v", err)
	}
	if restored.ServiceLimits() != policy {
		t.Fatal("reopen lost operational limits")
	}
}

func TestServiceLimitsYAMLStrictAndZeroDefault(t *testing.T) {
	raw := []byte("schema_version: 5\nworkspaces:\n  research:\n    routes: []\n")
	c, err := decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("research")
	w, _ := c.Workspace(slug)
	if w.ServiceLimits() != (routing.ServiceLimits{}) {
		t.Fatal("unconfigured limits not unlimited")
	}
	for _, body := range []string{
		"models: {requests_per_minute: 100001}",
		"mcp: {concurrent: 1}",
		"search: {concurrent: 1}",
		"models: {requests_per_minute: 2, unknown: 3}",
	} {
		source := []byte("schema_version: 5\nworkspaces:\n  research:\n    routes: []\n    service_limits:\n      " + body + "\n")
		if _, err := decode(source); err == nil {
			t.Fatalf("invalid YAML admitted: %s", source)
		}
	}
}
