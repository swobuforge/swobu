package bootstrap

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
)

// Mounting Admin into a Models-capable Workspace must not change an ordinary
// model request. Inspect the actual provider wire and the daemon's effect ledger.
func TestWorkspaceAdminMountDoesNotGraftIntoOrdinaryModels(t *testing.T) {
	t.Setenv("SWOBU_LEDGER", "sqlite")
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Tools []json.RawMessage `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Tools) != 0 {
			t.Error("ordinary Models request acquired tool declarations", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, workspaceToolRoundResponse("responses", "", true))
	}))
	defer provider.Close()
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "swobu.yaml")
	writeCurrentInferenceFixture(t, path, root, provider.URL, "responses", false)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	document := strings.Replace(string(raw), "  research:\n", "  research:\n    enabled_services: [models, mcp]\n    mcp_mounts:\n      node:\n        builtin: swobu-admin\n        allowed_tools: [workspace_create]\n", 1)
	if err := os.WriteFile(path, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	daemon, err := Start(t.Context(), StartInput{ConfigPath: path, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	slug, _ := routing.ParseWorkspaceSlug("research")
	_, issued, err := daemon.accessStore.Create("Models and MCP consumer", slug)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, daemon.BaseURL()+"/workspaces/research/v1/responses", strings.NewReader(`{"model":"chat","input":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+issued.Token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 || calls.Load() != 1 || !strings.Contains(string(body), "finished") {
		t.Fatalf("ordinary Models request: status=%d calls=%d body=%s err=%v", response.StatusCode, calls.Load(), body, err)
	}
	activity, err := operatorclient.New(http.DefaultClient, daemon.BaseURL()).Activity(t.Context(), "research")
	if err != nil || len(activity.MCPOperations) != 0 {
		t.Fatal("ordinary Models request invoked MCP", err)
	}
}
