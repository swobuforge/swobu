package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
)

// Every declared Workspace route must match its literal, method-aware mux
// registration. No configuration or command execution is needed.
func TestWorkspaceMuxMatchesDeclaredMigrationInventory(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "../../../../api/migration-inventory.json"))
	if err != nil {
		t.Fatal(err)
	}
	var inventory struct {
		Registrations []struct {
			Source     string `json:"source"`
			Expression string `json:"expression"`
		} `json:"registrations"`
	}
	if err := json.Unmarshal(raw, &inventory); err != nil {
		t.Fatal(err)
	}
	const source = "internal/adapters/inbound/httpapi/workspace_control_handler.go"
	mux := NewNodeControlAPIHandler(workspaces.Service{})
	count := 0
	for _, entry := range inventory.Registrations {
		if entry.Source != source {
			continue
		}
		pattern, err := strconv.Unquote(entry.Expression)
		if err != nil {
			t.Fatalf("Workspace routes must be literal strings: %q", entry.Expression)
		}
		pieces := strings.SplitN(pattern, " ", 2)
		if len(pieces) != 2 {
			t.Fatal(pattern)
		}
		path := strings.NewReplacer("{workspace}", "research", "{route}", "coding", "{alias}", "tools", "{service}", "models").Replace(pieces[1])
		_, matched := mux.Handler(httptest.NewRequest(pieces[0], path, nil))
		if matched != pattern {
			t.Errorf("%s matches %q", pattern, matched)
		}
		count++
	}
	if count == 0 {
		t.Fatal("no Workspace routes in inventory")
	}
	for _, path := range []string{"/_swobu/workspaces", "/api/v1/workspaces/research/routes/coding"} {
		_, matched := mux.Handler(httptest.NewRequest(http.MethodGet, path, nil))
		if matched != "" {
			t.Fatalf("retired %q registered as %q", path, matched)
		}
	}
}
