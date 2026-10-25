package bootstrap

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
)

type recordingTransport struct {
	next  http.RoundTripper
	paths []string
}

func (transport *recordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.paths = append(transport.paths, request.URL.RequestURI())
	return transport.next.RoundTrip(request)
}

func TestOperatorReadsCanonicalWorkspaceInventoryWithoutLegacyStatus(t *testing.T) {
	t.Setenv("SWOBU_LEDGER", "off")
	rootDir := t.TempDir()
	if err := os.Chmod(rootDir, 0o700); err != nil {
		t.Fatal(err)
	}
	daemon, err := Start(t.Context(), StartInput{
		ConfigPath:    filepath.Join(rootDir, "swobu.yaml"),
		StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()

	intent, err := operatorclient.NewWorkspaceCreationIntent(workspaces.CreateWorkspace{
		Slug: "research", InitialRoute: "chat",
		Target: workspaces.TargetDraft{
			ID: "primary", Model: "gpt-5", Protocol: "responses",
			ConnectionID: publishModelFixture(t, daemon, routing.ConnectionDraft{Provider: "openai", Standard: &routing.StandardConnectionDraft{Credential: "env:DIAGNOSTICS_SECRET_SENTINEL"}}),
		},
	}, "diagnostics-journey-create")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := operatorclient.New(http.DefaultClient, daemon.BaseURL()).ApplyWorkspaceCreation(t.Context(), intent); err != nil {
		t.Fatal(err)
	}

	transport := &recordingTransport{next: http.DefaultTransport}
	httpClient := &http.Client{Transport: transport}
	client := operatorclient.New(httpClient, daemon.BaseURL())
	list, err := client.ListWorkspaces(t.Context())
	if err != nil || len(list) != 1 || list[0].Slug != "research" {
		t.Fatalf("canonical Workspace list=%#v, %v", list, err)
	}
	workspace, err := client.GetWorkspace(t.Context(), "research")
	if err != nil || workspace.Slug != "research" || len(workspace.Routes) != 1 || workspace.Routes[0].Name != "chat" {
		t.Fatalf("canonical Workspace projection=%#v, %v", workspace, err)
	}
	for _, path := range transport.paths {
		if strings.Contains(path, "status-projection") {
			t.Fatalf("Operator requested unmounted status projection: %s", path)
		}
		if strings.HasPrefix(path, "/_swobu/workspaces") {
			t.Fatalf("Operator used legacy Workspace Control path: %s", path)
		}
	}
	if !containsPathPrefix(transport.paths, "/api/v1/workspaces") || !containsPathPrefix(transport.paths, "/api/v1/workspaces/research") {
		t.Fatalf("Operator did not read canonical Workspace Control resources: %v", transport.paths)
	}
}

func containsPathPrefix(paths []string, prefix string) bool {
	for _, path := range paths {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
