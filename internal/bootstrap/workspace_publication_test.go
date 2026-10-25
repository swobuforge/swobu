package bootstrap

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
)

// Exercise the mounted read/unpublish contract against the real Node writer.
// Publication setup uses its owner directly; no hosted certificate is invented.
func TestDaemonPublicationAndRelayIdentitySurviveRestart(t *testing.T) {
	t.Setenv("SWOBU_RELAY_ADDR", "127.0.0.1:1")
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "swobu.yaml")
	seedTestWorkspaces(t, path, "research")
	input := StartInput{ConfigPath: path, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}}
	daemon, err := Start(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	ws, _ := routing.ParseWorkspaceSlug("research")
	unlock := daemon.publicationStore.LockLifecycle()
	err = daemon.publicationStore.Set(ws, true, daemon.publicationStore.Revision(), func() error { return nil })
	unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err = daemon.relayStore.EnsureEndpoint(); err != nil {
		t.Fatal(err)
	}
	endpoint, err := daemon.relayStore.EndpointID()
	if err != nil {
		t.Fatal(err)
	}
	if err = daemon.Close(); err != nil {
		t.Fatal(err)
	}
	daemon, err = Start(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := daemon.relayStore.EndpointID()
	if err != nil || restored != endpoint {
		t.Fatal("restart changed relay identity", err)
	}
	call := func(method, suffix, revision string) (*http.Response, []byte) {
		t.Helper()
		request, err := http.NewRequestWithContext(t.Context(), method, daemon.BaseURL()+suffix, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("If-Match", revision)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("cacheable publication")
		}
		return response, raw
	}
	resource := "/api/v1/workspaces/research/publications"
	response, raw := call("GET", resource, "")
	var state struct {
		Published bool `json:"published"`
	}
	if response.StatusCode != 200 || json.Unmarshal(raw, &state) != nil || !state.Published {
		t.Fatalf("restart lost publication: %d %s", response.StatusCode, raw)
	}
	response, raw = call("DELETE", resource+"/relay", response.Header.Get("ETag"))
	if response.StatusCode != 200 || json.Unmarshal(raw, &state) != nil || state.Published {
		t.Fatalf("unpublish failed: %d %s", response.StatusCode, raw)
	}
	if daemon.publicationStore.HasPublicWorkspace() {
		t.Fatal("unpublished Workspace remained reachable")
	}
	if err = daemon.Close(); err != nil {
		t.Fatal(err)
	}
	daemon, err = Start(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if daemon.publicationStore.HasPublicWorkspace() {
		t.Fatal("restart resurrected publication")
	}
}
