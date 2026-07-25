package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	credentialsadapter "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	"github.com/swobuforge/swobu/internal/platform/config"
)

func TestDaemonPersistsAndResolvesPastedZAICredentialAcrossTargetReload(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvSwobuHome, root)
	t.Setenv(config.EnvAuthCredentialWritePolicy, "file")
	configPath := filepath.Join(root, "config", "swobu.yaml")
	daemon, err := Start(context.Background(), StartInput{
		ConfigPath: configPath, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	command := `{"provider_spec":"zai","name":"cockpit/target/personal/zai/target","secret":"zai-secret"}`
	response := requestWorkspace(t, http.MethodPost, daemon.BaseURL()+"/_swobu/credentials", command)
	if response.status != http.StatusOK {
		t.Fatalf("credential store status=%d body=%s", response.status, response.body)
	}
	var result struct {
		CredentialRef string `json:"credential_ref"`
	}
	if err := json.Unmarshal([]byte(response.body), &result); err != nil {
		t.Fatal(err)
	}
	if result.CredentialRef != "secretfile:cockpit/target/personal/zai/target" {
		t.Fatalf("credential ref=%q", result.CredentialRef)
	}
	command = fmt.Sprintf(
		`{"slug":"zai-persisted","initial_route":"chat","target":{"id":"primary","model":"manual-model","connection":{"zai":{"access":"coding_plan","credential":%q}}}}`,
		result.CredentialRef,
	)
	response = requestWorkspace(t, http.MethodPost, daemon.BaseURL()+"/_swobu/workspaces", command)
	if response.status != http.StatusCreated {
		t.Fatalf("Z.AI target create status=%d body=%s", response.status, response.body)
	}
	if err := daemon.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"zai:", "access: coding_plan", "credential: " + result.CredentialRef} {
		if !bytes.Contains(raw, []byte(token)) {
			t.Fatalf("persisted Z.AI target missing %q:\n%s", token, raw)
		}
	}
	if bytes.Contains(raw, []byte("protocol:")) {
		t.Fatalf("persisted Z.AI target authored derived protocol:\n%s", raw)
	}

	restarted, err := Start(context.Background(), StartInput{
		ConfigPath: configPath, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	response = requestWorkspace(t, http.MethodGet, restarted.BaseURL()+"/_swobu/workspaces/zai-persisted", "")
	for _, token := range []string{`"provider":"zai"`, `"protocol":"chat_completions_stream"`, `"access":"coding_plan"`, result.CredentialRef} {
		if response.status != http.StatusOK || !strings.Contains(response.body, token) {
			t.Fatalf("reloaded Z.AI target missing %q: status=%d body=%s", token, response.status, response.body)
		}
	}

	token, err := credentialsadapter.NewResolver().ResolveCredential(context.Background(), "zai", result.CredentialRef)
	if err != nil {
		t.Fatalf("daemon-persisted reference did not resolve after restart boundary: %v", err)
	}
	if token != "zai-secret" {
		t.Fatal("daemon-persisted reference resolved to unexpected secret material")
	}
}

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
	if err := first.Close(); err != nil {
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
	defer second.Close()
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
	defer daemon.Close()
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
