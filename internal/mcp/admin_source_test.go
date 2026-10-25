package mcp

import (
	"context"
	"encoding/json"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestBuiltinAdminMountRejectsExternalSourceImpersonation(t *testing.T) {
	called := 0
	builtin, err := NewAdminSourceBinding([]AdminTool{{Definition: sdk.Tool{Name: "read", InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)}, Call: func(context.Context, json.RawMessage) (*sdk.CallToolResult, error) {
		called++
		return &sdk.CallToolResult{}, nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("ordinary")
	workspace, err := routing.RestoreWorkspaceWithMCPMounts(slug, nil, nil, map[string]routing.MCPMount{"any-alias": {Builtin: routing.AdminMCPSource, AllowedTools: []string{"read"}}})
	if err != nil {
		t.Fatal(err)
	}
	forged := SourceBinding{ID: builtin.ID, Source: builtin.Source, NativeTools: builtin.NativeTools, Call: builtin.Call}
	if _, err := WorkspaceCatalog(workspace, map[string]SourceBinding{forged.ID: forged}); err == nil {
		t.Fatal("external source forged builtin dispatch marker")
	}
	catalog, err := WorkspaceCatalog(workspace, map[string]SourceBinding{builtin.ID: builtin})
	if err != nil || len(catalog) != 1 {
		t.Fatalf("catalog: %#v %v", catalog, err)
	}
	if _, err := CallWorkspaceTool(t.Context(), workspace, map[string]SourceBinding{builtin.ID: builtin}, catalog[0], json.RawMessage(`{}`)); err != nil || called != 1 {
		t.Fatalf("builtin call: %d %v", called, err)
	}
	for _, mount := range []routing.MCPMount{
		{Builtin: routing.AdminMCPSource, ConnectionID: "swobu-admin"},
		{Builtin: "invented"},
	} {
		if _, err := routing.RestoreWorkspaceWithMCPMounts(slug, nil, nil, map[string]routing.MCPMount{"node": mount}); err == nil {
			t.Fatalf("ambiguous/unrecognized source admitted: %#v", mount)
		}
	}
	// Changing the mount variant after selection must reject the frozen entry,
	// even when the external source advertises the same label and native tools.
	external, err := routing.RestoreWorkspaceWithMCPMounts(slug, nil, nil, map[string]routing.MCPMount{"any-alias": {ConnectionID: "swobu-admin", AllowedTools: []string{"read"}}})
	if err != nil {
		t.Fatal(err)
	}
	forged.ID, forged.ConnectionID, forged.ConnectionRevision = "swobu-admin", "swobu-admin", "1"
	if _, err := CallWorkspaceTool(t.Context(), external, map[string]SourceBinding{forged.ID: forged}, catalog[0], json.RawMessage(`{}`)); err == nil || called != 1 {
		t.Fatal("frozen builtin name rebound to external source")
	}
}
