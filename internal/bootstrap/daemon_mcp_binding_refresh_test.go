package bootstrap

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	credentials "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/testkit/connectionfixture"
)

func TestMCPBindingRefreshChild(t *testing.T) {
	if os.Getenv("SWOBU_BINDING_CHILD") != "1" {
		return
	}
	server := sdk.NewServer(&sdk.Implementation{Name: "refresh-fixture", Version: "1"}, nil)
	server.AddTool(&sdk.Tool{Name: "fixture", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: os.Getenv("TOKEN")}}}, nil
	})
	if err := server.Run(context.Background(), &sdk.StdioTransport{}); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

// A failed replacement must invalidate the old session, then recover from a
// later valid revision without changing the Workspace's Connection reference.
func TestMCPBindingRefreshFailsClosedAndRecovers(t *testing.T) {
	t.Setenv("SWOBU_REFRESH_MODE", "1")
	t.Setenv("SWOBU_REFRESH_TOKEN", "synthetic-old")
	t.Setenv("SWOBU_REFRESH_MISSING", "")
	registry, err := connectionfixture.Open(filepath.Join(t.TempDir(), "swobu.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	makeSpec := func(ref string) connections.MCPStdioSpec {
		spec, err := connections.NewMCPStdioSpec(executable, []string{"-test.run=^TestMCPBindingRefreshChild$"}, map[string]credentialref.Ref{"SWOBU_BINDING_CHILD": credentialref.Parse("env:SWOBU_REFRESH_MODE"), "TOKEN": credentialref.Parse(ref)})
		if err != nil {
			t.Fatal(err)
		}
		return spec
	}
	id, _ := connectionidentity.Parse("imported-docs")
	original, err := connections.New(id, "Docs", makeSpec("env:SWOBU_REFRESH_TOKEN"), time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Publish(original, connections.Revision{}); err != nil {
		t.Fatal(err)
	}
	resolver := credentials.NewResolver()
	binding, err := mcp.OpenStdioConnectionBinding(t.Context(), original, resolver.ResolveScopedCredential)
	if err != nil {
		t.Fatal(err)
	}
	var closed atomic.Int32
	closeOriginal := binding.Close
	binding.Close = func() error { closed.Add(1); return closeOriginal() }
	daemon := &Daemon{connectionStore: registry, mcpBindings: map[string]mcp.SourceBinding{id.String(): binding}}
	t.Cleanup(func() { closeMCPBindings(daemon.mcpBindings) })
	slug, _ := routing.ParseWorkspaceSlug("research")
	workspace, err := routing.RestoreWorkspaceWithMCPMounts(slug, nil, nil, map[string]routing.MCPMount{"docs": {ConnectionID: id.String(), AllowedTools: []string{"fixture"}}})
	if err != nil {
		t.Fatal(err)
	}
	call := func(want string) {
		t.Helper()
		bindings := daemon.currentMCPBindings()
		catalog, err := mcp.WorkspaceCatalog(workspace, bindings)
		if err != nil || len(catalog) != 1 {
			t.Fatalf("catalog=%v err=%v", catalog, err)
		}
		result, err := mcp.CallWorkspaceTool(t.Context(), workspace, bindings, catalog[0], json.RawMessage(`{}`))
		if err != nil || result == nil || len(result.Content) != 1 {
			t.Fatalf("call=%v err=%v", result, err)
		}
		text, ok := result.Content[0].(*sdk.TextContent)
		if !ok || text.Text != want {
			t.Fatalf("wrong revision credential: %v", result.Content)
		}
	}
	call("synthetic-old")
	broken, err := original.EditSameIdentity(original.Revision(), "Docs", makeSpec("env:SWOBU_REFRESH_MISSING"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Publish(broken, original.Revision()); err != nil {
		t.Fatal(err)
	}
	bindings := daemon.currentMCPBindings()
	if _, ok := bindings[id.String()]; ok {
		t.Fatal("failed revision refresh retained stale MCP session")
	}
	if closed.Load() != 1 {
		t.Fatalf("stale session closed %d times", closed.Load())
	}
	if _, err := mcp.WorkspaceCatalog(workspace, bindings); err == nil {
		t.Fatal("failed revision remained callable")
	}
	recovered, err := broken.EditSameIdentity(broken.Revision(), "Docs", makeSpec("env:SWOBU_REFRESH_TOKEN"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Publish(recovered, broken.Revision()); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SWOBU_REFRESH_TOKEN", "synthetic-new")
	call("synthetic-new")
	if daemon.mcpBindings[id.String()].ConnectionRevision != recovered.Revision().String() {
		t.Fatal("recovery did not bind current revision")
	}
}
