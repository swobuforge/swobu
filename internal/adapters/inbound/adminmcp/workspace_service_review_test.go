package adminmcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/nodefile"
	"github.com/swobuforge/swobu/internal/routing"
)

// The Admin MCP adapter is a separate caller of the canonical application
// owner. It must not bypass the human authority review or manufacture approval
// simply because the Workspace has a preexisting broad Principal grant.
func TestAdminMCPServiceActivationRequiresHumanReview(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "swobu.yaml")
	store, err := configstore.OpenOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	node, err := nodefile.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InstallNodePublisher(node.PublishRouting); err != nil {
		t.Fatal(err)
	}
	access, err := accessstate.OpenInline(nil, node.PublishPrincipals)
	if err != nil {
		t.Fatal(err)
	}
	service, err := workspaces.NewService(store, workspaces.Dependencies{Access: access})
	if err != nil {
		t.Fatal(err)
	}
	admit := func(context.Context) error { return nil }
	workspace, _, err := service.CreateWorkspaceChecked(t.Context(), workspaces.CreateWorkspace{Slug: "research"}, "operator", "create", admit)
	if err != nil {
		t.Fatal(err)
	}
	slug, _ := routing.ParseWorkspaceSlug("research")
	principal, _, err := access.Create("Shared agent", slug)
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithAuthority(t.Context(), principal.ID, admit)
	var read, write func(context.Context, json.RawMessage) (*sdk.CallToolResult, error)
	for _, tool := range WorkspaceTools(service) {
		switch tool.Definition.Name {
		case "workspace_services":
			read = tool.Call
		case "workspace_service_set":
			write = tool.Call
			var schema struct {
				Properties map[string]json.RawMessage `json:"properties"`
			}
			encoded, err := json.Marshal(tool.Definition.InputSchema)
			if err != nil || json.Unmarshal(encoded, &schema) != nil || len(schema.Properties["review_digest"]) != 0 {
				t.Fatal("Admin MCP must not expose an agent-self-approved expansion token", err)
			}
		}
	}
	if read == nil || write == nil {
		t.Fatal("required tools not registered")
	}
	call := func(body string) (*sdk.CallToolResult, error) { return write(ctx, json.RawMessage(body)) }
	base := `{"workspace":"research","service":"models","enabled":true,"etag":"` + workspace.ManagementETag + `"`
	unreviewed, err := call(base + `}`)
	if err != nil || !unreviewed.IsError {
		t.Fatalf("Admin MCP silently widened a grant: %+v %v", unreviewed, err)
	}
	result, err := read(ctx, json.RawMessage(`{"workspace":"research"}`))
	if err != nil || result.IsError || len(result.Content) == 0 {
		t.Fatalf("preview unavailable: %+v %v", result, err)
	}
	content, ok := result.Content[0].(*sdk.TextContent)
	if !ok {
		t.Fatal("Admin MCP must return a structured preview")
	}
	var rows []workspaces.ServiceState
	if err := json.Unmarshal([]byte(content.Text), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].ActivationReview == nil || !rows[0].ActivationReview.RequiresConfirmation || len(rows[0].ActivationReview.AffectedPrincipals) != 1 {
		t.Fatalf("Agent preview did not disclose impact: %+v", rows)
	}
	// A review digest is advisory, not proof of human consent. Admin MCP
	// cannot self-submit it even if a model learned it from workspace_services.
	forged, err := call(base + `,"review_digest":"` + rows[0].ActivationReview.Digest + `"}`)
	if err == nil || forged != nil {
		t.Fatalf("Admin MCP accepted or swallowed an unrecognized review token: %+v %v", forged, err)
	}
	// Control mutation remains denied after the preview; a local operator must
	// explicitly confirm in the browser. The Principal grant is not expanded.
	again, err := call(base + `}`)
	if err != nil || !again.IsError {
		t.Fatalf("Admin MCP used a preview as implicit approval: %+v %v", again, err)
	}
	current, err := service.GetWorkspace(t.Context(), "research")
	if err != nil || len(current.EnabledServices) != 0 {
		t.Fatalf("denied activation mutated current Workspace: %+v %v", current, err)
	}
}
