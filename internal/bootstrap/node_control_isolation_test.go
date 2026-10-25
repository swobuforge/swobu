package bootstrap

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
)

func requestWorkspaceWithETag(t *testing.T, method, url, body, etag string) workspaceResponse {
	t.Helper()
	request, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("If-Match", `"`+etag+`"`)
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

func TestDaemonNodeControlIsNotExposedThroughWorkspaceDataPath(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "swobu.yaml")
	seedTestWorkspaces(t, configPath, "research")
	daemon, err := Start(context.Background(), StartInput{
		ConfigPath:    configPath,
		StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	client := http.Client{}
	for _, path := range []string{"/api/v1/workspaces", "/workspaces/example/api/v1/workspaces", "/workspaces/example/_swobu/status"} {
		request, err := http.NewRequest(http.MethodGet, daemon.BaseURL()+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if (path == "/workspaces/example/api/v1/workspaces" || path == "/workspaces/example/_swobu/status") && response.StatusCode == http.StatusOK {
			t.Fatalf("workspace data path exposed Node Control: %s returned %d", path, response.StatusCode)
		}
	}
}

func TestDaemonManagedWorkspaceCredentialCannotReachWritableControlNamespaces(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "swobu.yaml")
	seedTestWorkspaces(t, configPath, "research")
	daemon, err := Start(context.Background(), StartInput{
		ConfigPath:    configPath,
		StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()

	workspace, err := routing.ParseWorkspaceSlug("research")
	if err != nil {
		t.Fatal(err)
	}
	_, issued, err := daemon.accessStore.Create("managed-client", workspace)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/v1/workspaces", `{}`},
		{http.MethodPatch, "/api/v1/workspaces/research", `{}`},
		{http.MethodDelete, "/api/v1/workspaces/research", `{}`},
		{http.MethodPost, "/_swobu/workspaces", `{}`},
		{http.MethodPost, "/_swobu/credentials", `{}`},
		{http.MethodPost, "/api/v1/principals", `{}`},
		{http.MethodPut, "/api/v1/workspaces/research/permissions/missing", `{}`},
		{http.MethodGet, "/api/v1/remote-addresses", ``},
		{http.MethodGet, "/api/v1/workspaces/research/publications", ``},
		{http.MethodPut, "/api/v1/workspaces/research/publications/relay", ``},
		{http.MethodDelete, "/api/v1/workspaces/research/publications/relay", ``},
	}
	for _, testCase := range cases {
		t.Run(testCase.method+" "+testCase.path, func(t *testing.T) {
			request, err := http.NewRequest(testCase.method, daemon.BaseURL()+testCase.path, strings.NewReader(testCase.body))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer "+issued.Token)
			request.Header.Set("Content-Type", "application/json")
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			expected := http.StatusForbidden
			if strings.HasPrefix(testCase.path, "/_swobu/") {
				expected = http.StatusNotFound
			}
			if response.StatusCode != expected {
				t.Fatalf("managed Workspace credential reached %s: status=%d", testCase.path, response.StatusCode)
			}
		})
	}
}

func TestDaemonProjectsSuccessfulExchangeThroughActivityEndpoint(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "swobu.yaml")
	daemon, err := Start(context.Background(), StartInput{
		ConfigPath:    configPath,
		StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()

	intent, err := operatorclient.NewWorkspaceCreationIntent(workspaces.CreateWorkspace{
		Slug: "activity-check", InitialRoute: "chat",
		Target: workspaces.TargetDraft{ID: "primary", Model: "gpt-5", Protocol: "responses", ConnectionID: publishModelFixture(t, daemon, routing.ConnectionDraft{Provider: "openai", Standard: &routing.StandardConnectionDraft{Credential: "env:OPENAI_API_KEY"}})},
	}, "activity-fixture-creation")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := operatorclient.New(http.DefaultClient, daemon.BaseURL()).ApplyWorkspaceCreation(t.Context(), intent); err != nil {
		t.Fatal(err)
	}
	response := requestWorkspace(t, http.MethodGet, daemon.BaseURL()+"/api/v1/activity?workspace=activity-check", "")
	if response.status != http.StatusOK {
		t.Fatalf("activity status=%d body=%s", response.status, response.body)
	}
	if !strings.Contains(response.body, `"scope":{"kind":"workspace","workspace":"activity-check"}`) || !strings.Contains(response.body, `"exchanges":[]`) {
		t.Fatalf("activity projection = %s", response.body)
	}
	workspace, err := routing.ParseWorkspaceSlug("activity-check")
	if err != nil {
		t.Fatal(err)
	}
	_, issued, err := daemon.accessStore.Create("activity-client", workspace)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodGet, daemon.BaseURL()+"/api/v1/activity?workspace=activity-check", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+issued.Token)
	deniedResponse, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer deniedResponse.Body.Close()
	body, err := io.ReadAll(deniedResponse.Body)
	if err != nil {
		t.Fatal(err)
	}
	if deniedResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("Workspace bearer reached canonical Activity: status=%d body=%s", deniedResponse.StatusCode, body)
	}
}

func TestDaemonRetiresLegacyActivityControlRoute(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	daemon, err := Start(context.Background(), StartInput{
		ConfigPath:    filepath.Join(root, "swobu.yaml"),
		StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()

	canonical := requestWorkspace(t, http.MethodGet, daemon.BaseURL()+"/api/v1/activity", "")
	if canonical.status != http.StatusOK || !strings.Contains(canonical.body, `"scope":{"kind":"all"}`) {
		t.Fatalf("canonical Node Activity status=%d body=%s", canonical.status, canonical.body)
	}

	legacy, err := http.Get(daemon.BaseURL() + "/_swobu/activity")
	if err != nil {
		t.Fatal(err)
	}
	_ = legacy.Body.Close()
	if legacy.StatusCode != http.StatusNotFound {
		t.Fatalf("retired legacy Activity route status=%d, want 404", legacy.StatusCode)
	}

}

func TestDaemonDoesNotMountDuplicateAccessCollection(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	daemon, err := Start(context.Background(), StartInput{
		ConfigPath:    filepath.Join(root, "swobu.yaml"),
		StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()

	principals := requestWorkspace(t, http.MethodGet, daemon.BaseURL()+"/api/v1/principals", "")
	if principals.status != http.StatusOK || !strings.HasPrefix(strings.TrimSpace(principals.body), "[") {
		t.Fatalf("canonical Principal collection status=%d body=%s", principals.status, principals.body)
	}
	access := requestWorkspace(t, http.MethodGet, daemon.BaseURL()+"/api/v1/access", "")
	if access.status != http.StatusNotFound {
		t.Fatalf("duplicate Access collection status=%d, want 404", access.status)
	}
	legacy := requestWorkspace(t, http.MethodGet, daemon.BaseURL()+"/_swobu/access", "")
	if legacy.status != http.StatusNotFound {
		t.Fatalf("retired Access collection still registered: status=%d body=%s", legacy.status, legacy.body)
	}
}

func TestDaemonSearchSourceAttachmentUsesCanonicalNodeControl(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	daemon, err := Start(context.Background(), StartInput{
		ConfigPath:    filepath.Join(root, "swobu.yaml"),
		StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	searchID, err := connectionidentity.New()
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := connections.ParseEndpoint("https://search-fixture.example.test/search")
	if err != nil {
		t.Fatal(err)
	}
	searchSpec, err := connections.NewSearchSpec("fixture", endpoint, credentialref.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	searchConnection, err := connections.New(searchID, "Search fixture", searchSpec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.connectionStore.Publish(searchConnection, connections.Revision{}); err != nil {
		t.Fatal(err)
	}
	mcpID, err := connectionidentity.New()
	if err != nil {
		t.Fatal(err)
	}
	mcpEndpoint, err := connections.ParseEndpoint("https://mcp-fixture.example.test/mcp")
	if err != nil {
		t.Fatal(err)
	}
	mcpSpec, err := connections.NewMCPHTTPSpec(mcpEndpoint, credentialref.Ref{})
	if err != nil {
		t.Fatal(err)
	}
	mcpSpec, err = mcpSpec.WithOperatorDeclarations("fixture account", "fixture tool access")
	if err != nil {
		t.Fatal(err)
	}
	mcpConnection, err := connections.New(mcpID, "MCP fixture", mcpSpec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.connectionStore.Publish(mcpConnection, connections.Revision{}); err != nil {
		t.Fatal(err)
	}

	created := requestWorkspace(t, http.MethodPost, daemon.BaseURL()+"/api/v1/workspaces", `{"slug":"search-cutover"}`, "search-cutover-workspace")
	if created.status != http.StatusCreated {
		t.Fatalf("canonical Workspace create status=%d body=%s", created.status, created.body)
	}
	slug, err := routing.ParseWorkspaceSlug("search-cutover")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.configStore.Update(t.Context(), func(current routing.Config) (routing.Config, error) {
		next, err := current.SetServiceEnabled(slug, routing.ServiceSearch, true)
		if err != nil {
			return routing.Config{}, err
		}
		return next.SetServiceEnabled(slug, routing.ServiceMCP, true)
	}); err != nil {
		t.Fatal(err)
	}

	current := requestWorkspace(t, http.MethodGet, daemon.BaseURL()+"/api/v1/workspaces/search-cutover", "")
	var currentProjection struct {
		ManagementETag string `json:"management_etag"`
	}
	if err := json.Unmarshal([]byte(current.body), &currentProjection); err != nil {
		t.Fatal(err)
	}
	attach := requestWorkspaceWithETag(t, http.MethodPut, daemon.BaseURL()+"/api/v1/workspaces/search-cutover/search-route", `{"connection_id":"`+searchID.String()+`","fallback_connection_ids":[]}`, currentProjection.ManagementETag)
	if attach.status != http.StatusOK || !strings.Contains(attach.body, `"search":"configured"`) {
		t.Fatalf("canonical search-source attach status=%d body=%s", attach.status, attach.body)
	}
	checkRequest, err := http.NewRequestWithContext(t.Context(), http.MethodPost, daemon.BaseURL()+"/api/v1/connections/"+searchID.String()+"/checks", nil)
	if err != nil {
		t.Fatal(err)
	}
	checkRequest.Header.Set("If-Match", `"1"`)
	checkResponse, err := http.DefaultClient.Do(checkRequest)
	if err != nil {
		t.Fatal(err)
	}
	checkBody, err := io.ReadAll(checkResponse.Body)
	_ = checkResponse.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if checkResponse.StatusCode != http.StatusOK || !strings.Contains(string(checkBody), `"connection_id":"`+searchID.String()+`"`) || !strings.Contains(string(checkBody), `"connection_revision":"1"`) || strings.Contains(string(checkBody), "search-fixture.example.test") {
		t.Fatalf("Connection Search check status=%d body=%s", checkResponse.StatusCode, checkBody)
	}

	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		legacy := requestWorkspace(t, method, daemon.BaseURL()+"/_swobu/workspaces/search-cutover/search-source", `{"source_id":"legacy-source"}`)
		if legacy.status != http.StatusNotFound {
			t.Fatalf("legacy search-source %s status=%d, want 404 (body=%s)", method, legacy.status, legacy.body)
		}
	}

	operator := operatorclient.New(http.DefaultClient, daemon.BaseURL())
	preview, err := operator.PreviewMCPConnectionMount(t.Context(), "search-cutover", "github", mcpID.String(), []string{"read_file"})
	if err != nil {
		t.Fatal(err)
	}
	if preview.ConnectionID != mcpID.String() {
		t.Fatalf("canonical MCP preview Connection=%q", preview.ConnectionID)
	}
	mounted, err := operator.ApplyMCPConnectionMount(t.Context(), "search-cutover", "github", mcpID.String(), []string{"read_file"}, workspaces.MCPMountReviewRequest{WorkspaceETag: preview.Revision, Digest: preview.Digest, ConnectionRevision: preview.ConnectionRevision, AccessRevision: preview.AccessRevision, PublicationRevision: preview.PublicationRevision})
	if err != nil || mounted.Slug != "search-cutover" {
		t.Fatalf("canonical MCP apply workspace=%#v err=%v", mounted, err)
	}
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		legacy := requestWorkspace(t, method, daemon.BaseURL()+"/_swobu/workspaces/search-cutover/mcp-sources/github", `{"integration":"legacy","allow_tools":["read_file"]}`)
		if legacy.status != http.StatusNotFound {
			t.Fatalf("legacy MCP-source %s status=%d, want 404 (body=%s)", method, legacy.status, legacy.body)
		}
	}

	workspace := requestWorkspace(t, http.MethodGet, daemon.BaseURL()+"/api/v1/workspaces/search-cutover", "")
	if workspace.status != http.StatusOK || !strings.Contains(workspace.body, `"search":"configured"`) || !strings.Contains(workspace.body, `"connection_id":"`+mcpID.String()+`"`) {
		t.Fatalf("canonical Workspace readback status=%d body=%s", workspace.status, workspace.body)
	}

	current = requestWorkspace(t, http.MethodGet, daemon.BaseURL()+"/api/v1/workspaces/search-cutover", "")
	if err := json.Unmarshal([]byte(current.body), &currentProjection); err != nil {
		t.Fatal(err)
	}
	mcpRemove := requestWorkspaceWithETag(t, http.MethodDelete, daemon.BaseURL()+"/api/v1/workspaces/search-cutover/mcp-mounts/github", "", currentProjection.ManagementETag)
	if mcpRemove.status != http.StatusOK || strings.Contains(mcpRemove.body, `"connection_id":"`+mcpID.String()+`"`) {
		t.Fatalf("canonical MCP-source removal status=%d body=%s", mcpRemove.status, mcpRemove.body)
	}

	current = requestWorkspace(t, http.MethodGet, daemon.BaseURL()+"/api/v1/workspaces/search-cutover", "")
	if err := json.Unmarshal([]byte(current.body), &currentProjection); err != nil {
		t.Fatal(err)
	}
	removed := requestWorkspaceWithETag(t, http.MethodDelete, daemon.BaseURL()+"/api/v1/workspaces/search-cutover/search-route", "", currentProjection.ManagementETag)
	if removed.status != http.StatusOK || strings.Contains(removed.body, `"search":"configured"`) {
		t.Fatalf("canonical search-source removal status=%d body=%s", removed.status, removed.body)
	}

	workspaceSlug, err := routing.ParseWorkspaceSlug("search-cutover")
	if err != nil {
		t.Fatal(err)
	}
	_, credential, err := daemon.accessStore.Create("search-cutover-client", workspaceSlug)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPut, daemon.BaseURL()+"/api/v1/workspaces/search-cutover/search-route", strings.NewReader(`{"connection_id":"`+searchID.String()+`","fallback_connection_ids":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+credential.Token)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("Workspace credential reached Node Search-source control: status=%d", response.StatusCode)
	}

	request, err = http.NewRequest(http.MethodPut, daemon.BaseURL()+"/api/v1/workspaces/search-cutover/mcp-mounts/github", strings.NewReader(`{"connection_id":"`+mcpID.String()+`","selected_tools":["read_file"]}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+credential.Token)
	request.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("Workspace credential reached Node MCP-source control: status=%d", response.StatusCode)
	}
}
