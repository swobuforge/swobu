package bootstrap

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/platform/config"
)

func TestWorkspaceCommandPersistsAcrossDaemonRestart(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	initial := "schema_version: 1\nworkspaces: {}\n"
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	first, err := Start(context.Background(), StartInput{ConfigPath: path, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	command := `{"slug":"persisted","initial_route":"chat","target":{"id":"primary","model":"gpt-5","protocol":"responses","connection":{"openai":{"credential":"env:OPENAI_API_KEY"}}}}`
	response := requestWorkspace(t, http.MethodPost, first.BaseURL()+"/_swobu/workspaces", command)
	if response.status != http.StatusCreated || !strings.Contains(response.body, `"slug":"persisted"`) {
		t.Fatalf("create status=%d body=%s", response.status, response.body)
	}
	if err := first.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"schema_version: 1", "persisted:", "default_route: chat", "id: primary", "protocol: responses"} {
		if !bytes.Contains(raw, []byte(token)) {
			t.Fatalf("persisted YAML missing %q:\n%s", token, raw)
		}
	}

	second, err := Start(context.Background(), StartInput{ConfigPath: path, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(context.Background())
	response = requestWorkspace(t, http.MethodGet, second.BaseURL()+"/_swobu/workspaces/persisted", "")
	if response.status != http.StatusOK || !strings.Contains(response.body, `"default_route":"chat"`) || !strings.Contains(response.body, `"id":"primary"`) {
		t.Fatalf("restored status=%d body=%s", response.status, response.body)
	}
}

func TestStartCreatesMissingRoutingConfigThroughStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "swobu.yaml")
	daemon, err := Start(context.Background(), StartInput{ConfigPath: path, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close(context.Background())
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "schema_version: 1\nworkspaces: {}\n" {
		t.Fatalf("initial document = %q", raw)
	}
}

type workspaceResponse struct {
	status int
	body   string
}

func requestWorkspace(t *testing.T, method, url, body string) workspaceResponse {
	t.Helper()
	request, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return workspaceResponse{status: response.StatusCode, body: string(raw)}
}
