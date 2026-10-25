package bootstrap

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	evidenceStore "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestAliceAndBobWorkspacePermissionsAndScopedKeysSurviveRevocationAndRestart(t *testing.T) {
	t.Setenv("SWOBU_LEDGER", "sqlite")
	object, err := canonical.ParseJSONObject([]byte(`{"type":"object","properties":{"query":{"type":"string"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	toolKey, _ := canonical.NewToolKey("mcp/docs", canonical.ToolKindFunction, "search")
	tool, err := canonical.NewFunctionTool(toolKey, "Search docs", canonical.NewToolSchemaObject(object), canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled})
	if err != nil {
		t.Fatal(err)
	}
	sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "docs")
	declaration, err := canonical.NewMCPToolSource(sourceKey, "", canonical.MCPSource{}, []canonical.ToolDeclaration{tool})
	if err != nil {
		t.Fatal(err)
	}
	source, _ := declaration.MCP()
	var effects atomic.Int32
	bindings := map[string]mcp.SourceBinding{
		"cn_fixture_docs": {
			ID: "cn_fixture_docs", ConnectionID: "cn_fixture_docs", ConnectionRevision: "1", Source: source,
			Call: func(_ context.Context, name string, arguments json.RawMessage) (*sdk.CallToolResult, error) {
				effects.Add(1)
				if name != "search" || string(arguments) != `{"query":"private request"}` {
					return nil, os.ErrInvalid
				}
				return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "private result"}}}, nil
			},
		},
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	publishMCPFixtureConnection(t, path, "cn_fixture_docs")
	start := func() *Daemon {
		t.Helper()
		daemon, err := Start(t.Context(), StartInput{ConfigPath: path, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}, MCPConnectionBindings: bindings})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = daemon.Close() })
		return daemon
	}
	daemon := start()
	control := operatorclient.New(http.DefaultClient, daemon.BaseURL())
	for _, slug := range []string{"coding", "research"} {
		intent, err := operatorclient.NewWorkspaceCreationIntent(workspaces.CreateWorkspace{Slug: slug}, "create-"+slug)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := control.ApplyWorkspaceCreation(t.Context(), intent); err != nil {
			t.Fatal(err)
		}
		workspace, err := routing.ParseWorkspaceSlug(slug)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := daemon.configStore.Update(t.Context(), func(current routing.Config) (routing.Config, error) {
			next, err := current.SetServiceEnabled(workspace, routing.ServiceMCP, true)
			if err != nil {
				return routing.Config{}, err
			}
			return next.SetMCPMount(workspace, "docs", routing.MCPMount{ConnectionID: "cn_fixture_docs", AllowedTools: []string{"search"}})
		}); err != nil {
			t.Fatal(err)
		}
	}
	alice, _, err := control.CreatePrincipal(t.Context(), "Alice", "create-alice")
	if err != nil {
		t.Fatal(err)
	}
	bob, _, err := control.CreatePrincipal(t.Context(), "Bob", "create-bob")
	if err != nil || alice.ID == bob.ID {
		t.Fatalf("independent identities missing: %v", err)
	}
	permission := func(principal, workspace string, grant bool, key string) {
		t.Helper()
		preview, err := control.PreviewWorkspacePermission(t.Context(), workspace, principal, grant, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		reviewed, err := operatorclient.ReviewPermission(preview, key)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := control.ApplyWorkspacePermission(t.Context(), reviewed); err != nil {
			t.Fatal(err)
		}
	}
	issue := func(principal, workspace, key string) accessapp.IssueResult {
		t.Helper()
		intent, err := operatorclient.RestoreCredentialIssuanceIntent(principal, workspace, key)
		if err != nil {
			t.Fatal(err)
		}
		result, err := control.ApplyCredentialIssuance(t.Context(), intent)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	permission(alice.ID, "coding", true, "alice-coding")
	permission(alice.ID, "research", true, "alice-research")
	permission(bob.ID, "research", true, "bob-research")
	aliceCoding := issue(alice.ID, "coding", "alice-coding-key")
	aliceResearch := issue(alice.ID, "research", "alice-research-revoked-key")
	aliceResearchSibling := issue(alice.ID, "research", "alice-research-sibling-key")
	bobResearch := issue(bob.ID, "research", "bob-research-key")
	probe := func(token, resource string, want int) {
		t.Helper()
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, daemon.BaseURL()+resource, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("resource %s status=%d want=%d", resource, response.StatusCode, want)
		}
	}
	verify := func(aliceResearchAllowed, aliceResearchSiblingAllowed bool) {
		t.Helper()
		probe(aliceCoding.Token, "/workspaces/coding/v1/models", http.StatusOK)
		probe(bobResearch.Token, "/workspaces/research/v1/models", http.StatusOK)
		want := http.StatusUnauthorized
		if aliceResearchAllowed {
			want = http.StatusOK
		}
		probe(aliceResearch.Token, "/workspaces/research/v1/models", want)
		want = http.StatusUnauthorized
		if aliceResearchSiblingAllowed {
			want = http.StatusOK
		}
		probe(aliceResearchSibling.Token, "/workspaces/research/v1/models", want)
		probe(aliceCoding.Token, "/workspaces/research/v1/models", http.StatusForbidden)
		wrongWorkspaceStatus := http.StatusUnauthorized
		if aliceResearchAllowed {
			wrongWorkspaceStatus = http.StatusForbidden
		}
		probe(aliceResearch.Token, "/workspaces/coding/v1/models", wrongWorkspaceStatus)
		probe(bobResearch.Token, "/workspaces/coding/v1/models", http.StatusForbidden)
		probe(bobResearch.Token, "/api/v1/principals", http.StatusForbidden)
	}
	verify(true, true)
	for _, scope := range []struct {
		token, workspace string
	}{{aliceCoding.Token, "research"}, {aliceResearch.Token, "coding"}, {bobResearch.Token, "coding"}} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, daemon.BaseURL()+"/workspaces/"+scope.workspace+"/mcp", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+scope.token)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal("wrong-Workspace MCP probe failed")
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusNotFound || effects.Load() != 0 {
			t.Fatalf("wrong-Workspace MCP status=%d effects=%d", response.StatusCode, effects.Load())
		}
	}
	connect := func(token, workspace string) *sdk.ClientSession {
		t.Helper()
		client := sdk.NewClient(&sdk.Implementation{Name: "principal-journey", Version: "1"}, nil)
		session, err := client.Connect(t.Context(), &sdk.StreamableClientTransport{
			Endpoint:             daemon.BaseURL() + "/workspaces/" + workspace + "/mcp",
			HTTPClient:           &http.Client{Transport: &bearerRoundTripper{next: http.DefaultTransport, token: token}},
			DisableStandaloneSSE: true,
		}, nil)
		if err != nil {
			t.Fatal("authorized SDK connection failed")
		}
		t.Cleanup(func() { _ = session.Close() })
		return session
	}
	call := func(session *sdk.ClientSession, allowed bool) {
		t.Helper()
		before := effects.Load()
		result, err := session.CallTool(t.Context(), &sdk.CallToolParams{Name: "docs__search", Arguments: map[string]any{"query": "private request"}})
		if !allowed {
			if err == nil && (result == nil || !result.IsError) {
				t.Fatal("revoked permission still dispatched a native tool")
			}
			if effects.Load() != before {
				t.Fatal("denied tool call reached source")
			}
			return
		}
		if err != nil || result == nil || result.IsError || len(result.Content) != 1 || effects.Load() != before+1 {
			t.Fatalf("authorized native tool did not execute exactly once: result=%#v err=%v effects=%d before=%d", result, err, effects.Load(), before)
		}
		content, ok := result.Content[0].(*sdk.TextContent)
		if !ok || content.Text != "private result" {
			t.Fatal("native tool result changed")
		}
	}
	aliceCodingSession := connect(aliceCoding.Token, "coding")
	aliceResearchSession := connect(aliceResearch.Token, "research")
	aliceResearchSiblingSession := connect(aliceResearchSibling.Token, "research")
	bobResearchSession := connect(bobResearch.Token, "research")
	for _, session := range []*sdk.ClientSession{aliceCodingSession, aliceResearchSession, aliceResearchSiblingSession, bobResearchSession} {
		listed, err := session.ListTools(t.Context(), nil)
		if err != nil || len(listed.Tools) != 1 || listed.Tools[0].Name != "docs__search" {
			t.Fatal("authorized native catalog missing")
		}
		call(session, true)
	}
	credentialPreview, err := control.PreviewCredentialRevocation(t.Context(), alice.ID, aliceResearch.Credential.ID)
	if err != nil {
		t.Fatal(err)
	}
	credentialIntent, err := operatorclient.RestoreCredentialRevocationIntent(alice.ID, aliceResearch.Credential.ID, credentialPreview.Revision, "revoke-alice-research-credential")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.ApplyCredentialRevocation(t.Context(), credentialIntent); err != nil {
		t.Fatal(err)
	}
	verify(false, true)
	call(aliceResearchSession, false)
	call(aliceResearchSiblingSession, true)
	call(aliceCodingSession, true)
	call(bobResearchSession, true)
	permission(alice.ID, "research", false, "revoke-alice-research")
	verify(false, false)
	call(aliceResearchSession, false)
	call(aliceResearchSiblingSession, false)
	if _, err := aliceResearchSession.ListTools(t.Context(), nil); err == nil {
		t.Fatal("revoked session still reads catalog")
	}
	call(aliceCodingSession, true)
	call(bobResearchSession, true)
	activity := operatorclient.New(http.DefaultClient, daemon.BaseURL())
	before := make(map[string]operatorclient.ActivityProjection)
	for _, workspace := range []string{"coding", "research"} {
		projection, err := activity.Activity(t.Context(), workspace)
		want := 3
		if workspace == "research" {
			want = 6
		}
		if err != nil || len(projection.Exchanges) != 0 || len(projection.MCPOperations) != want || projection.History != nil {
			t.Fatalf("Workspace %s lost isolated standalone history", workspace)
		}
		actors := map[string]int{}
		for _, operation := range projection.MCPOperations {
			if operation.Workspace != workspace || operation.Source != "cn_fixture_docs" || operation.Tool != "search" || operation.Outcome != execution.Succeeded || operation.Occurrence != execution.Occurred || operation.OperationID == "" || operation.SettledAt == "" {
				t.Fatal("standalone operation lost identity or effect truth")
			}
			actors[operation.AccessID]++
		}
		wantActors := map[string]int{alice.ID: 3}
		if workspace == "research" {
			wantActors = map[string]int{alice.ID: 3, bob.ID: 3}
		}
		if !reflect.DeepEqual(actors, wantActors) {
			t.Fatal("standalone operations lost Principal attribution")
		}
		before[workspace] = projection
	}
	if err := daemon.Close(); err != nil {
		t.Fatal(err)
	}
	if health := daemon.ledgerWriter.Health(); health.CommittedMCPOperationsTotal != 9 || health.DroppedMCPOperationsTotal != 0 || effects.Load() != 9 {
		t.Fatal("shutdown lost admitted source evidence or recorded a denied effect")
	}
	daemon = start()
	control = operatorclient.New(http.DefaultClient, daemon.BaseURL())
	verify(false, false)
	if len(daemon.executionEvidenceStore.Settled()) != 0 {
		t.Fatal("restart retained in-memory Exchanges")
	}
	activity = operatorclient.New(http.DefaultClient, daemon.BaseURL())
	if live := daemon.executionEvidenceStore.ProjectActivity(evidenceStore.ProjectionInput{}); len(live.MCPOperations) != 0 {
		t.Fatal("restart retained in-memory MCP history")
	}
	for workspace, original := range before {
		restored, err := activity.Activity(t.Context(), workspace)
		if err != nil || !reflect.DeepEqual(original, restored) || effects.Load() != 9 {
			t.Fatalf("restart changed durable Workspace %s history or reexecuted source", workspace)
		}
	}
	for _, scope := range []struct {
		credential accessapp.IssueResult
		workspace  string
		allowed    bool
	}{{aliceCoding, "coding", true}, {aliceResearch, "research", false}, {aliceResearchSibling, "research", false}, {bobResearch, "research", true}} {
		if !scope.allowed {
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, daemon.BaseURL()+"/workspaces/"+scope.workspace+"/mcp", nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer "+scope.credential.Token)
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal("revoked credential restart probe failed")
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusUnauthorized {
				t.Fatal("restart restored revoked MCP authority")
			}
		} else {
			call(connect(scope.credential.Token, scope.workspace), true)
		}
	}
	for _, scope := range []struct {
		principal, workspace string
		allowed              bool
	}{{alice.ID, "coding", true}, {alice.ID, "research", false}, {bob.ID, "research", true}, {bob.ID, "coding", false}} {
		state, err := control.GetWorkspacePermission(t.Context(), scope.workspace, scope.principal)
		if err != nil || state.Allowed != scope.allowed {
			t.Fatalf("permission %s/%s allowed=%v want=%v err=%v", scope.principal, scope.workspace, state.Allowed, scope.allowed, err)
		}
	}
	aliceCurrent, err := control.GetPrincipal(t.Context(), alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	principalReview, err := control.PreviewPrincipalRevocation(t.Context(), alice.ID, aliceCurrent.Revision)
	if err != nil {
		t.Fatal(err)
	}
	principalIntent, err := operatorclient.RestorePrincipalRevocationIntent(alice.ID, principalReview.Principal.Revision, "revoke-alice-after-restart")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.ApplyPrincipalRevocation(t.Context(), principalIntent); err != nil {
		t.Fatal(err)
	}
	for _, credential := range []accessapp.IssueResult{aliceCoding, aliceResearch, aliceResearchSibling} {
		probe(credential.Token, "/workspaces/coding/v1/models", http.StatusUnauthorized)
	}
	probe(bobResearch.Token, "/workspaces/research/v1/models", http.StatusOK)
	if err := daemon.Close(); err != nil {
		t.Fatal(err)
	}
	daemon = start()
	control = operatorclient.New(http.DefaultClient, daemon.BaseURL())
	for _, credential := range []accessapp.IssueResult{aliceCoding, aliceResearch, aliceResearchSibling} {
		probe(credential.Token, "/workspaces/coding/v1/models", http.StatusUnauthorized)
	}
	probe(bobResearch.Token, "/workspaces/research/v1/models", http.StatusOK)
	if current, err := control.GetPrincipal(t.Context(), alice.ID); err != nil || current.Status != "revoked" {
		t.Fatal("Principal revocation did not survive a second daemon reopen")
	}
}
