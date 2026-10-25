package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/configstore"
)

func TestWorkspaceDetailPublishesDurableExactManagementRevision(t *testing.T) {
	for _, revision := range []string{"12", "9007199254740992"} {
		t.Run(revision, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "swobu.yaml")
			if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces:\n  dev:\n    management_revision: "+revision+"\n    routes: []\n"), 0600); err != nil {
				t.Fatal(err)
			}
			store, err := configstore.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			service, err := workspaces.NewService(store, workspaces.Dependencies{})
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			NewNodeControlAPIHandler(service).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces/dev", nil))
			if revision == "12" {
				var detail workspaces.Workspace
				if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
					t.Fatal(err)
				}
				if response.Code != 200 || !strings.HasSuffix(detail.ManagementETag, "-12") || response.Header().Get("ETag") != `"`+detail.ManagementETag+`"` || !strings.Contains(response.Body.String(), `"management_revision":12`) {
					t.Fatalf("revision detail: %d %s", response.Code, response.Body.String())
				}
			} else if response.Code != 503 || response.Header().Get("ETag") != "" {
				t.Fatal("unsafe integer projected")
			}
		})
	}
}
