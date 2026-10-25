package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	connectapp "github.com/swobuforge/swobu/internal/app/operator/connect"
	"github.com/swobuforge/swobu/internal/clientconnect"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestClientConfigurationPreviewETagTracksRealFileReview(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("CODEX_HOME", directory)
	path := filepath.Join(directory, "config.toml")
	const original = "model = \"old-model\"\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	service, err := connectapp.NewService(clientconnect.NewService(), connectTestCredentials{})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewLocalAdmissionHandler(NewClientConfigurationHandler(service, nil, "http://127.0.0.1:7926", func(_ *http.Request, slug routing.WorkspaceSlug, modelID string) bool {
		return slug.String() == "research" && modelID == "coding"
	}), nil, nil)
	preview := func() string {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/client-configurations?dry_run=true", strings.NewReader(`{"workspace":"research","model_id":"coding","client":"codex"}`))
		request.Header.Set("Content-Type", "application/json")
		request.RemoteAddr = "127.0.0.1:1234"
		request.Host = "127.0.0.1:7926"
		writer := httptest.NewRecorder()
		handler.ServeHTTP(writer, request)
		var plan connectapp.Plan
		if writer.Code != http.StatusOK || json.Unmarshal(writer.Body.Bytes(), &plan) != nil || plan.Revision == "" || writer.Header().Get("ETag") != `"`+plan.Revision+`"` || writer.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("preview omitted matching review revision/ETag")
		}
		return plan.Revision
	}
	first := preview()
	if first != preview() {
		t.Fatal("unchanged file review changed revision")
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != original {
		t.Fatal("preview changed configuration")
	}
	if err := os.WriteFile(path, []byte("model = \"concurrent-model\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if first == preview() {
		t.Fatal("changed semantic review retained revision")
	}
}
