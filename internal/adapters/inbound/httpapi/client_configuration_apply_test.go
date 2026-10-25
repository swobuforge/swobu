package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	connectapp "github.com/swobuforge/swobu/internal/app/operator/connect"
	"github.com/swobuforge/swobu/internal/clientconnect"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestCanonicalClientConfigurationReviewedApplyAndOriginalReplay(t *testing.T) {
	root := t.TempDir()
	clientDir := filepath.Join(root, "client")
	attemptDir := filepath.Join(root, "attempts")
	for _, directory := range []string{clientDir, attemptDir} {
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CODEX_HOME", clientDir)
	path := filepath.Join(clientDir, "config.toml")
	const original = "model = \"old\"\nunrelated = \"keep\"\n[model_providers.swobu.http_headers]\nAuthorization=\"Bearer fixture-client-token\"\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	service, err := connectapp.NewService(clientconnect.NewService(), connectTestCredentials{})
	if err != nil {
		t.Fatal(err)
	}
	attempts, err := connectapp.OpenAttempts(attemptDir)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewLocalAdmissionHandler(NewClientConfigurationHandler(service, attempts, "http://127.0.0.1:7926", func(_ *http.Request, slug routing.WorkspaceSlug, modelID string) bool {
		return slug.String() == "research" && modelID == "coding"
	}), nil, nil)
	request := func(query string, body any, match, key string) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/client-configurations?"+query, bytes.NewReader(raw))
		req.RemoteAddr = "127.0.0.1:1234"
		req.Host = "127.0.0.1:7926"
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", match)
		req.Header.Set("Idempotency-Key", key)
		writer := httptest.NewRecorder()
		handler.ServeHTTP(writer, req)
		return writer
	}
	preview := request("dry_run=true", map[string]string{"workspace": "research", "model_id": "coding", "client": "codex", "principal": "acc_AAAAAAAAAAAAAAAAAAAAAA"}, "", "")
	var plan connectapp.Plan
	if preview.Code != http.StatusOK || json.Unmarshal(preview.Body.Bytes(), &plan) != nil || plan.Revision == "" {
		t.Fatal("canonical preview failed")
	}
	review := plan.Review()
	body := struct {
		Workspace string            `json:"workspace"`
		ModelID   string            `json:"model_id"`
		Client    string            `json:"client"`
		Principal string            `json:"principal"`
		Review    connectapp.Review `json:"review"`
	}{"research", "coding", "codex", "acc_AAAAAAAAAAAAAAAAAAAAAA", review}
	const key = "canonical-connect-original-key"
	for _, query := range []string{"", "dry_run=invalid", "dry_run=false&dry_run=true", "dry_run=false&extra=true"} {
		if result := request(query, body, preview.Header().Get("ETag"), key); result.Code != http.StatusBadRequest {
			t.Fatal("ambiguous apply query admitted")
		}
	}
	rawDenied, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	deniedRequest := httptest.NewRequest(http.MethodPost, "/api/v1/client-configurations?dry_run=false", bytes.NewReader(rawDenied))
	deniedRequest.Header.Set("Content-Type", "application/json")
	deniedRequest.Header.Set("If-Match", preview.Header().Get("ETag"))
	deniedRequest.Header.Set("Idempotency-Key", key)
	denied := httptest.NewRecorder()
	NewClientConfigurationHandler(service, attempts, "http://127.0.0.1:7926", nil).ServeHTTP(denied, deniedRequest)
	if denied.Code != http.StatusForbidden {
		t.Fatal("unadmitted apply reached owner")
	}
	for _, negative := range []struct {
		match, key string
		status     int
	}{
		{"", key, http.StatusPreconditionRequired},
		{`"wrong"`, key, http.StatusPreconditionFailed},
		{preview.Header().Get("ETag"), "", http.StatusBadRequest},
	} {
		if result := request("dry_run=false", body, negative.match, negative.key); result.Code != negative.status {
			t.Fatal("invalid apply precondition admitted")
		}
	}
	const concurrent = "model = \"concurrent\"\nunrelated = \"keep\"\n"
	if err := os.WriteFile(path, []byte(concurrent), 0600); err != nil {
		t.Fatal(err)
	}
	if result := request("dry_run=false", body, preview.Header().Get("ETag"), key); result.Code != http.StatusPreconditionFailed {
		t.Fatal("stale reviewed file admitted")
	}
	entries, err := os.ReadDir(attemptDir)
	if err != nil || len(entries) != 0 {
		t.Fatal("stale preview retained an attempt")
	}
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	first := request("dry_run=false", body, preview.Header().Get("ETag"), key)
	var committed connectapp.ApplyResult
	if first.Code != http.StatusOK || json.Unmarshal(first.Body.Bytes(), &committed) != nil || committed.Replayed || !committed.AlreadyConfigured || first.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("canonical reviewed apply not acknowledged")
	}
	const later = "model = \"later\"\n"
	if err := os.WriteFile(path, []byte(later), 0600); err != nil {
		t.Fatal(err)
	}
	replayed := request("dry_run=false", body, preview.Header().Get("ETag"), key)
	var recovery connectapp.ApplyResult
	if replayed.Code != http.StatusOK || json.Unmarshal(replayed.Body.Bytes(), &recovery) != nil || !recovery.Replayed || recovery.Revision != committed.Revision {
		t.Fatal("canonical retry did not recover original completion")
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != later {
		t.Fatal("replay overwrote later client configuration")
	}
	body.Review.Changes[0].Before = "different"
	if changed := request("dry_run=false", body, preview.Header().Get("ETag"), key); changed.Code == http.StatusOK {
		t.Fatal("changed key intent replayed")
	}
}
