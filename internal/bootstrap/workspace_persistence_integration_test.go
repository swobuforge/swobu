package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	credentialsadapter "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/sharestate"
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

func TestStartRevokesShareBindingsForRoutingIdentityMissingAtRestart(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "swobu.yaml")
	first, err := Start(context.Background(), StartInput{ConfigPath: configPath, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	command := `{"slug":"prod","initial_route":"coding","target":{"id":"primary","model":"gpt-5","protocol":"responses","connection":{"openai":{"credential":"env:OPENAI_API_KEY"}}}}`
	response := requestWorkspace(t, http.MethodPost, first.BaseURL()+"/_swobu/workspaces", command)
	if response.status != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.status, response.body)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	presentConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	sharePath := filepath.Join(dir, "share.json")
	shares, err := sharestate.Open(sharePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := shares.EnsureEndpoint(); err != nil {
		t.Fatal(err)
	}
	workspace, _ := routing.ParseWorkspaceSlug("prod")
	route, _ := routing.ParseRouteName("coding")
	workspaceGrant, err := shares.Issue(workspace, routing.RouteName{}, sharestate.ExpiryNever)
	if err != nil {
		t.Fatal(err)
	}
	routeGrant, err := shares.Issue(workspace, route, sharestate.ExpiryNever)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(configPath, []byte("schema_version: 1\nworkspaces: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	observedMissing, err := Start(context.Background(), StartInput{ConfigPath: configPath, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := observedMissing.Close(); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(configPath, presentConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	recreated, err := Start(context.Background(), StartInput{ConfigPath: configPath, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := recreated.Close(); err != nil {
		t.Fatal(err)
	}
	reconciled, err := sharestate.Open(sharePath)
	if err != nil {
		t.Fatal(err)
	}
	for scope, bearer := range map[string]string{"workspace": workspaceGrant.Bearer, "route": routeGrant.Bearer} {
		if _, err := reconciled.Authenticate(bearer); !errors.Is(err, sharestate.ErrUnauthorized) {
			t.Fatalf("%s Share bearer survived observed routing identity destruction: %v", scope, err)
		}
	}
}

func TestStartRevokesShareBindingForRouteMissingAtRestart(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "swobu.yaml")
	first, err := Start(context.Background(), StartInput{ConfigPath: configPath, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	command := `{"slug":"prod","initial_route":"coding","target":{"id":"primary","model":"gpt-5","protocol":"responses","connection":{"openai":{"credential":"env:OPENAI_API_KEY"}}}}`
	response := requestWorkspace(t, http.MethodPost, first.BaseURL()+"/_swobu/workspaces", command)
	if response.status != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.status, response.body)
	}
	command = `{"workspace":"prod","name":"review","target":{"id":"review-primary","model":"gpt-5","protocol":"responses","connection":{"openai":{"credential":"env:OPENAI_API_KEY"}}}}`
	response = requestWorkspace(t, http.MethodPost, first.BaseURL()+"/_swobu/workspaces/prod/routes", command)
	if response.status != http.StatusOK {
		t.Fatalf("create route status=%d body=%s", response.status, response.body)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	presentConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}

	sharePath := filepath.Join(dir, "share.json")
	shares, err := sharestate.Open(sharePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := shares.EnsureEndpoint(); err != nil {
		t.Fatal(err)
	}
	workspace, _ := routing.ParseWorkspaceSlug("prod")
	review, _ := routing.ParseRouteName("review")
	grant, err := shares.Issue(workspace, review, sharestate.ExpiryNever)
	if err != nil {
		t.Fatal(err)
	}

	missingRouteConfig := bytes.Replace(presentConfig, []byte("      review:\n"), []byte("      removed:\n"), 1)
	if bytes.Equal(missingRouteConfig, presentConfig) {
		t.Fatalf("persisted config did not contain review route:\n%s", presentConfig)
	}
	if err := os.WriteFile(configPath, missingRouteConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	observedMissing, err := Start(context.Background(), StartInput{ConfigPath: configPath, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := observedMissing.Close(); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(configPath, presentConfig, 0o600); err != nil {
		t.Fatal(err)
	}
	recreated, err := Start(context.Background(), StartInput{ConfigPath: configPath, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := recreated.Close(); err != nil {
		t.Fatal(err)
	}
	reconciled, err := sharestate.Open(sharePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reconciled.Authenticate(grant.Bearer); !errors.Is(err, sharestate.ErrUnauthorized) {
		t.Fatalf("route Share bearer survived observed route identity destruction: %v", err)
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
