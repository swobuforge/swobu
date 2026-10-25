package bootstrap

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
)

// Ordered routes must survive the whole Node lifecycle, including Connection
// admission and real Control mux wiring, not only a standalone codec roundtrip.
func TestDaemonModelRoutePrioritySurvivesReopen(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	input := StartInput{ConfigPath: filepath.Join(root, "swobu.yaml"), StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}}
	daemon, err := Start(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if daemon != nil {
			_ = daemon.Close()
		}
	})
	connection := publishModelFixture(t, daemon, routing.ConnectionDraft{Provider: "openai", Standard: &routing.StandardConnectionDraft{Credential: "env:UNUSED_PRIORITY_FIXTURE"}})
	slug, _ := routing.ParseWorkspaceSlug("priority")
	if _, err := daemon.configStore.Update(t.Context(), func(c routing.Config) (routing.Config, error) {
		next, err := c.CreateEmptyWorkspace(routing.EmptyWorkspaceSeed{Slug: slug})
		if err != nil {
			return routing.Config{}, err
		}
		return next.SetServiceEnabled(slug, routing.ServiceModels, true)
	}); err != nil {
		t.Fatal(err)
	}
	send := func(method, path, body string) string {
		t.Helper()
		workspace, _ := daemon.configStore.Config().Workspace(slug)
		request, err := http.NewRequestWithContext(t.Context(), method, daemon.BaseURL()+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("If-Match", fmt.Sprintf("\"%s-%d\"", workspace.ManagementIncarnation(), workspace.ManagementRevision()))
		request.Header.Set("Idempotency-Key", "priority-"+path)
		request.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != 200 {
			t.Fatalf("%s %s: %d %s", method, path, response.StatusCode, raw)
		}
		return string(raw)
	}
	for i, pattern := range []string{"coding", "claude-*", "*"} {
		send(http.MethodPut, "/api/v1/workspaces/priority/model-routes/"+pattern, fmt.Sprintf(`{"tiers":[{"targets":[{"id":"target-%d","model":"fixed-model","protocol":"responses","connection_id":%q}]}]}`, i, connection))
	}
	send(http.MethodPatch, "/api/v1/workspaces/priority/model-routes", `{"order":["claude-*","coding","*"]}`)
	before, _ := daemon.configStore.Config().Workspace(slug)
	if err := daemon.Close(); err != nil {
		t.Fatal(err)
	}
	daemon, err = Start(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := daemon.configStore.Config().Workspace(slug)
	var order []string
	for _, route := range after.Routes() {
		order = append(order, route.Name().String())
	}
	if !slices.Equal(order, []string{"claude-*", "coding", "*"}) || after.ManagementRevision() != before.ManagementRevision() {
		t.Fatalf("reopen changed priority/revision: %v", order)
	}
	var match struct {
		Matched  bool
		Pattern  string
		Priority int
	}
	if err := json.Unmarshal([]byte(send(http.MethodGet, "/api/v1/workspaces/priority/model-match?model=claude-sonnet-4", "")), &match); err != nil {
		t.Fatal(err)
	}
	if !match.Matched || match.Pattern != "claude-*" || match.Priority != 1 {
		t.Fatalf("reopened matcher: %+v", match)
	}
}
