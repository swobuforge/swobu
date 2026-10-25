package bootstrap

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
)

// This proof uses the compiled daemon and official external SDK. There is no
// injected builtin binding, local-host proxy, provider or model dependency.
func TestWorkspaceAdminMCPReadReviewedCreateAndLiveRevocation(t *testing.T) {
	t.Setenv("SWOBU_LEDGER", "sqlite")
	t.Setenv("SWOBU_ADMIN_CHILD", "1")
	t.Setenv("SWOBU_ADMIN_SOURCE_TOKEN", "synthetic-source-only")
	root := t.TempDir()
	t.Setenv(config.EnvSwobuHome, root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "clients"))
	if err := os.MkdirAll(filepath.Join(root, "clients", "opencode"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "clients", "opencode", "opencode.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces:\n  operator-tools:\n    enabled_services: [mcp]\n    routes: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	daemon, err := Start(t.Context(), StartInput{ConfigPath: path, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	control := operatorclient.New(http.DefaultClient, daemon.BaseURL())
	principal, _, err := control.CreatePrincipal(t.Context(), "Delegated operator", "create-admin-principal")
	if err != nil {
		t.Fatal(err)
	}
	grant := func(id string, allow bool) {
		t.Helper()
		preview, err := control.PreviewWorkspacePermission(t.Context(), "operator-tools", id, allow, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		review, err := operatorclient.ReviewPermission(preview, map[bool]string{true: "grant-admin-", false: "revoke-admin-"}[allow]+id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := control.ApplyWorkspacePermission(t.Context(), review); err != nil {
			t.Fatal(err)
		}
	}
	grant(principal.ID, true)
	credentialIntent, err := operatorclient.RestoreCredentialIssuanceIntent(principal.ID, "operator-tools", "admin-key")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := control.ApplyCredentialIssuance(t.Context(), credentialIntent)
	if err != nil {
		t.Fatal(err)
	}
	// Mount through ordinary local Control preview/apply. A broad existing grant
	// is visible in that review; builtin authority is never selected by alias.
	selected := make([]string, 0, len(daemon.adminMCPBinding.NativeTools))
	for name := range daemon.adminMCPBinding.NativeTools {
		selected = append(selected, name)
	}
	sort.Strings(selected)
	body := map[string]any{"builtin": "swobu-admin", "selected_tools": selected}
	put := func(dryRun bool, review *workspaces.MCPMountPreview) []byte {
		t.Helper()
		if review == nil {
			delete(body, "review")
		}
		if review != nil {
			body["review"] = workspaces.MCPMountReviewRequest{WorkspaceETag: review.Revision, Digest: review.Digest, ConnectionRevision: review.ConnectionRevision, AccessRevision: review.AccessRevision, PublicationRevision: review.PublicationRevision}
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPut, daemon.BaseURL()+"/api/v1/workspaces/operator-tools/mcp-mounts/node?dry_run="+map[bool]string{true: "true", false: "false"}[dryRun], bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		if review != nil {
			request.Header.Set("If-Match", `"`+review.Revision+`"`)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		result, _ := io.ReadAll(response.Body)
		if response.StatusCode != 200 {
			t.Fatalf("mount status %d: %s", response.StatusCode, result)
		}
		return result
	}
	var preview workspaces.MCPMountPreview
	if err := json.Unmarshal(put(true, nil), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Builtin != routing.AdminMCPSource || !preview.IdentityVerified || len(preview.AffectedPrincipals) != 1 || preview.AffectedPrincipals[0].ID != principal.ID {
		t.Fatalf("builtin delegation missing from review: %#v", preview)
	}
	put(false, &preview)
	client := sdk.NewClient(&sdk.Implementation{Name: "admin-parity-proof", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &sdk.StreamableClientTransport{Endpoint: daemon.BaseURL() + "/workspaces/operator-tools/mcp", HTTPClient: &http.Client{Transport: &bearerRoundTripper{next: http.DefaultTransport, token: credential.Token}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	catalog, err := session.ListTools(t.Context(), nil)
	if err != nil || len(catalog.Tools) != len(selected) {
		t.Fatalf("catalog %#v: %v", catalog, err)
	}
	secretMaterials := []string{credential.Token}
	call := func(name string, args map[string]any, allowed bool) *sdk.CallToolResult {
		t.Helper()
		result, err := session.CallTool(t.Context(), &sdk.CallToolParams{Name: "node__" + name, Arguments: args})
		if allowed && (err != nil || result == nil || result.IsError) {
			t.Fatalf("%s failed: %#v %v", name, result, err)
		}
		if !allowed && err == nil && result != nil && !result.IsError {
			t.Fatalf("denied %s succeeded", name)
		}
		if result != nil {
			raw, _ := json.Marshal(result)
			for _, secret := range secretMaterials {
				if strings.Contains(string(raw), secret) {
					t.Fatal("credential leaked to model")
				}
			}
		}
		return result
	}
	call("workspaces_list", map[string]any{}, true)
	call("node_status", map[string]any{}, true)
	call("workspace_get", map[string]any{"workspace": "operator-tools"}, true)
	// A model-visible impact digest is advisory, never independent human
	// consent. Exercise the actual SDK transport and assembled authority owners.
	serviceResult := call("workspace_services", map[string]any{"workspace": "operator-tools"}, true)
	var serviceStates []workspaces.ServiceState
	if len(serviceResult.Content) != 1 {
		t.Fatal("Service review must be a structured result")
	}
	serviceText, ok := serviceResult.Content[0].(*sdk.TextContent)
	if !ok || json.Unmarshal([]byte(serviceText.Text), &serviceStates) != nil {
		t.Fatal("Service review could not be decoded")
	}
	if len(serviceStates) != 3 || serviceStates[0].ActivationReview == nil || !serviceStates[0].ActivationReview.RequiresConfirmation {
		t.Fatal("existing broad grant missing from activation review")
	}
	activation := serviceStates[0].ActivationReview
	activationArgs := map[string]any{"workspace": "operator-tools", "service": "models", "enabled": true, "etag": activation.WorkspaceETag}
	beforeActivation, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	call("workspace_service_set", activationArgs, false)
	activationArgs["review_digest"] = activation.Digest
	call("workspace_service_set", activationArgs, false)
	afterActivation, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(beforeActivation, afterActivation) {
		t.Fatal("denied SDK Service activation mutated Node state")
	}
	call("connections_list", map[string]any{"kind": "", "query": ""}, true)
	call("principals_list", map[string]any{}, true)
	call("principal_get", map[string]any{"id": principal.ID}, true)
	call("principal_credentials_list", map[string]any{"id": principal.ID}, true)
	call("workspace_limits_status", map[string]any{"workspace": "operator-tools"}, true)
	call("workspace_publication_get", map[string]any{"workspace": "operator-tools"}, true)
	call("remote_addresses_list", map[string]any{}, true)
	call("clients_list", map[string]any{"workspace": "operator-tools"}, true)
	call("activity_get", map[string]any{}, true)
	call("credential_issue_handoff", map[string]any{"principal": principal.ID, "workspace": "operator-tools"}, true)
	call("connection_create_handoff", map[string]any{}, true)
	// Complete the secure issuance handoff through the supported local consumer,
	// then require agent-visible metadata for that exact newly issued credential.
	hostIntent, err := operatorclient.RestoreCredentialIssuanceIntent(principal.ID, "operator-tools", "host-handoff-key")
	if err != nil {
		t.Fatal(err)
	}
	hostIssued, err := control.ApplyCredentialIssuance(t.Context(), hostIntent)
	if err != nil {
		t.Fatal(err)
	}
	secretMaterials = append(secretMaterials, hostIssued.Token)
	metadata := call("principal_credentials_list", map[string]any{"id": principal.ID}, true)
	if !strings.Contains(metadata.Content[0].(*sdk.TextContent).Text, hostIssued.Credential.ID) {
		t.Fatal("handoff did not expose exact completion metadata")
	}

	connectionArgs := map[string]any{"display_name": "Agent HTTP source", "kind": "mcp", "endpoint": "https://127.0.0.1:1/mcp", "provider_id": "", "execution_identity": "Test identity", "permission_summary": "Read fixture", "retry_key": "create-source", "dry_run": true}
	call("connection_create", connectionArgs, true)
	connectionArgs["dry_run"] = false
	connectionResult := call("connection_create", connectionArgs, true)
	var createdConnection struct {
		Connection struct {
			ID       string `json:"id"`
			Revision string `json:"revision"`
		} `json:"connection"`
	}
	if err := json.Unmarshal([]byte(connectionResult.Content[0].(*sdk.TextContent).Text), &createdConnection); err != nil {
		t.Fatal(err)
	}
	call("connection_create", connectionArgs, true)
	call("connection_get", map[string]any{"id": createdConnection.Connection.ID}, true)
	call("connection_check", map[string]any{"id": createdConnection.Connection.ID, "revision": createdConnection.Connection.Revision}, true)
	call("connection_references", map[string]any{"id": createdConnection.Connection.ID}, true)
	call("connection_credentials_handoff", map[string]any{"id": createdConnection.Connection.ID}, true)
	call("connection_delete", map[string]any{"id": createdConnection.Connection.ID, "revision": createdConnection.Connection.Revision}, true)
	limitArgs := map[string]any{"workspace": "operator-tools", "limit": map[string]any{"currency": "USD", "amount_cents": 100, "window": "day", "mode": "observe_only", "unknown_price": "reject"}, "dry_run": true, "review_id": ""}
	limitResult := call("workspace_limit_set", limitArgs, true)
	var limitReview struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(limitResult.Content[0].(*sdk.TextContent).Text), &limitReview); err != nil {
		t.Fatal(err)
	}
	limitArgs["dry_run"] = false
	limitArgs["review_id"] = limitReview.ID
	limitRead := call("workspace_limit_set", limitArgs, true)
	var limitSnapshot struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal([]byte(limitRead.Content[0].(*sdk.TextContent).Text), &limitSnapshot); err != nil {
		t.Fatal(err)
	}
	call("workspace_limit_clear", map[string]any{"workspace": "operator-tools", "revision": limitSnapshot.Revision}, true)
	policyAudits := daemon.accountingStore.LimitChanges("operator-tools")
	if len(policyAudits) != 1 || policyAudits[0].Actor != principal.ID {
		t.Fatal("reviewed policy lost original actor")
	}
	activityResult := call("activity_get", map[string]any{"workspace": "operator-tools"}, true)
	var activityRead struct {
		Operations []struct {
			ID string `json:"operation_id"`
		} `json:"mcp_operations"`
	}
	if err := json.Unmarshal([]byte(activityResult.Content[0].(*sdk.TextContent).Text), &activityRead); err != nil || len(activityRead.Operations) == 0 {
		t.Fatal("MCP evidence missing from current Activity")
	}
	call("activity_operation_get", map[string]any{"id": activityRead.Operations[0].ID}, true)
	call("activity_get", map[string]any{"workspace": ""}, false)
	call("activity_get", map[string]any{"workspace": "operator-tools", "kind": "mcp_operation", "limit": 1}, true)

	args := map[string]any{"slug": "agent-created", "display_name": "Created through Workspace MCP", "retry_key": "agent-create", "dry_run": true}
	call("workspace_create", args, true)
	slug, _ := routing.ParseWorkspaceSlug("agent-created")
	if _, err := daemon.configStore.GetWorkspace(t.Context(), slug); err == nil {
		t.Fatal("preview mutated Node")
	}
	args["dry_run"] = false
	call("workspace_create", args, true)
	call("workspace_create", args, true) // original Node receipt, same Principal
	created, err := control.GetWorkspace(t.Context(), "agent-created")
	if err != nil || created.DisplayName != "Created through Workspace MCP" {
		t.Fatalf("Control readback: %#v %v", created, err)
	}
	call("workspace_rename", map[string]any{"workspace": "agent-created", "display_name": "Renamed through Workspace MCP", "etag": created.ManagementETag}, true)
	nameAudits, err := daemon.configStore.WorkspaceDisplayNameAudits()
	if err != nil || len(nameAudits) != 1 || nameAudits[0].Caller != principal.ID {
		t.Fatalf("rename attribution lost: %#v %v", nameAudits, err)
	}
	audits, err := daemon.configStore.WorkspaceCreationAudits()
	if err != nil {
		t.Fatal(err)
	}
	if len(audits) != 1 || audits[0].Caller != "mcp:"+principal.ID+":workspace.create" {
		t.Fatalf("Principal audit lost: %#v", audits)
	}

	// Configure the other Services via the same admitted tools, reading current
	// owner ETags between distinct intents. No provider invocation is required.
	etag := func() string {
		t.Helper()
		value, err := control.GetWorkspace(t.Context(), "agent-created")
		if err != nil {
			t.Fatal(err)
		}
		return value.ManagementETag
	}
	for _, service := range []string{"models", "search"} {
		call("workspace_service_set", map[string]any{"workspace": "agent-created", "service": service, "enabled": true, "etag": etag()}, true)
	}
	connectionArgs["kind"] = "model"
	connectionArgs["display_name"] = "Agent custom Models"
	connectionArgs["endpoint"] = "https://example.invalid"
	connectionArgs["provider_id"] = "custom"
	connectionArgs["execution_identity"] = ""
	connectionArgs["permission_summary"] = ""
	connectionArgs["retry_key"] = "create-model"
	modelResult := call("connection_create", connectionArgs, true)
	var modelConnection struct {
		Connection struct {
			ID       string `json:"id"`
			Revision string `json:"revision"`
		} `json:"connection"`
	}
	if err := json.Unmarshal([]byte(modelResult.Content[0].(*sdk.TextContent).Text), &modelConnection); err != nil {
		t.Fatal(err)
	}
	route := map[string]any{"tiers": []any{map[string]any{"targets": []any{map[string]any{"id": "primary", "model": "test-model", "protocol": "responses", "connection_id": modelConnection.Connection.ID}}}}}
	call("model_route_put", map[string]any{"workspace": "agent-created", "route": "coding", "spec": route, "etag": etag()}, true)
	call("model_route_get", map[string]any{"workspace": "agent-created", "route": "coding"}, true)
	call("model_match", map[string]any{"workspace": "agent-created", "model": "coding"}, true)
	call("model_route_order", map[string]any{"workspace": "agent-created", "patterns": []string{"coding"}, "etag": etag()}, true)
	grantArgs := map[string]any{"workspace": "agent-created", "principal": principal.ID, "grant": true, "services": []string{"models"}, "revision": "", "dry_run": true, "retry_key": "client-grant"}
	grantResult := call("permission_set", grantArgs, true)
	var grantReview struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal([]byte(grantResult.Content[0].(*sdk.TextContent).Text), &grantReview); err != nil {
		t.Fatal(err)
	}
	grantArgs["revision"] = grantReview.Revision
	grantArgs["dry_run"] = false
	call("permission_set", grantArgs, true)
	call("workspace_permission_get", map[string]any{"workspace": "agent-created", "principal": principal.ID}, true)
	clientArgs := map[string]any{"workspace": "agent-created", "principal": principal.ID, "model": "coding", "client": "opencode"}
	call("client_preview", clientArgs, true)
	call("client_apply_handoff", clientArgs, true)
	hostPlan, err := control.PreviewClientConfiguration(t.Context(), "agent-created", "opencode", principal.ID, "coding")
	if err != nil {
		t.Fatal(err)
	}
	hostConnect, err := operatorclient.NewClientConfigurationIntent(hostPlan, "host-connect-handoff")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.ApplyClientConfiguration(t.Context(), hostConnect); err != nil {
		t.Fatal(err)
	}
	clientRead := call("client_preview", clientArgs, true)
	var clientStatus struct {
		AlreadyConfigured bool `json:"already_configured"`
	}
	if err := json.Unmarshal([]byte(clientRead.Content[0].(*sdk.TextContent).Text), &clientStatus); err != nil || !clientStatus.AlreadyConfigured {
		t.Fatal("secure host Connect completion did not verify")
	}
	connectionArgs["kind"] = "search"
	connectionArgs["provider_id"] = "brave"
	connectionArgs["display_name"] = "Agent Search"
	connectionArgs["retry_key"] = "create-search"
	searchResult := call("connection_create", connectionArgs, true)
	var searchConnection struct {
		Connection struct {
			ID       string `json:"id"`
			Revision string `json:"revision"`
		} `json:"connection"`
	}
	if err := json.Unmarshal([]byte(searchResult.Content[0].(*sdk.TextContent).Text), &searchConnection); err != nil {
		t.Fatal(err)
	}
	connectionArgs["retry_key"] = "create-search-backup"
	connectionArgs["display_name"] = "Agent Search backup"
	backupResult := call("connection_create", connectionArgs, true)
	var backupConnection struct {
		Connection struct {
			ID string `json:"id"`
		} `json:"connection"`
	}
	if err := json.Unmarshal([]byte(backupResult.Content[0].(*sdk.TextContent).Text), &backupConnection); err != nil {
		t.Fatal(err)
	}
	call("search_route_set", map[string]any{"workspace": "agent-created", "connection_id": searchConnection.Connection.ID, "fallback_connection_ids": []string{backupConnection.Connection.ID}, "etag": etag()}, true)
	searchRoute := call("search_route_get", map[string]any{"workspace": "agent-created"}, true)
	var searchRouteValue struct {
		ConnectionID string   `json:"connection_id"`
		Fallback     []string `json:"fallback_connection_ids"`
	}
	if err := json.Unmarshal([]byte(searchRoute.Content[0].(*sdk.TextContent).Text), &searchRouteValue); err != nil || searchRouteValue.ConnectionID != searchConnection.Connection.ID || len(searchRouteValue.Fallback) != 1 || searchRouteValue.Fallback[0] != backupConnection.Connection.ID {
		t.Fatalf("Admin MCP Search route projection: %+v %v", searchRouteValue, err)
	}
	call("search_route_set", map[string]any{"workspace": "agent-created", "connection_id": "", "fallback_connection_ids": []string{}, "etag": etag()}, true)

	call("model_route_delete", map[string]any{"workspace": "agent-created", "route": "coding", "etag": etag()}, true)
	// Secure host authoring can also provision a stdio source without sending
	// launch configuration through model arguments. The agent sees its catalog,
	// mounts an exact selected tool, then removes it at the original revisions.
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	hostSource := map[string]any{"kind": "mcp", "display_name": "Host authored source", "spec": map[string]any{"transport": "stdio", "execution_identity": "Fixture account", "permission_summary": "Fixture declarations only", "configuration": map[string]any{"executable": executable, "args": []string{"-test.run=^TestMCPBindingRefreshChild$"}, "env_refs": map[string]string{"SWOBU_BINDING_CHILD": "env:SWOBU_ADMIN_CHILD", "TOKEN": "env:SWOBU_ADMIN_SOURCE_TOKEN"}}}}
	// Delegated tool arguments cannot acquire the host's executable or file
	// authority. Use valid network-creation fields so refusal cannot be
	// explained by missing required inputs, and compare the durable Node.
	const fileSentinel = "synthetic-local-file-custody-only"
	credentialFile := filepath.Join(root, "host-credential.txt")
	if err := os.WriteFile(credentialFile, []byte(fileSentinel), 0600); err != nil {
		t.Fatal(err)
	}
	secretMaterials = append(secretMaterials, fileSentinel)
	beforeHostAuthoring, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, injection := range []struct {
		field string
		value any
	}{
		{"executable", executable},
		{"args", []string{"-test.run=^TestMCPBindingRefreshChild$"}},
		{"env", map[string]string{"TOKEN": "synthetic-agent-environment"}},
		{"env_refs", map[string]string{"TOKEN": "env:SWOBU_ADMIN_SOURCE_TOKEN"}},
		{"credential_path", credentialFile},
		{"spec", hostSource["spec"]},
	} {
		input := map[string]any{"display_name": "Rejected host authoring", "kind": "mcp", "endpoint": "https://example.invalid/mcp", "provider_id": "", "execution_identity": "Agent", "permission_summary": "Unapproved host access", "retry_key": "reject-" + injection.field, "dry_run": false}
		input[injection.field] = injection.value
		call("connection_create", input, false)
		call("connection_create_handoff", map[string]any{injection.field: injection.value}, false)
	}
	afterHostAuthoring, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(beforeHostAuthoring, afterHostAuthoring) {
		t.Fatal("rejected delegated host authoring changed Node state")
	}
	hostSourceRaw, _ := json.Marshal(hostSource)
	sourceRequest, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, daemon.BaseURL()+"/api/v1/connections", bytes.NewReader(hostSourceRaw))
	sourceRequest.Header.Set("Content-Type", "application/json")
	sourceRequest.Header.Set("Idempotency-Key", "host-source-handoff")
	sourceResponse, err := http.DefaultClient.Do(sourceRequest)
	if err != nil {
		t.Fatal(err)
	}
	var authored struct {
		Connection struct {
			ID       string `json:"id"`
			Revision string `json:"revision"`
		} `json:"connection"`
	}
	if sourceResponse.StatusCode != http.StatusCreated {
		sourceResponse.Body.Close()
		t.Fatal("host source creation failed")
	}
	if err := json.NewDecoder(sourceResponse.Body).Decode(&authored); err != nil {
		t.Fatal(err)
	}
	sourceResponse.Body.Close()
	// A valid handoff identifies the existing Connection; it cannot become a
	// credential-file read or rotation command by carrying extra host fields.
	beforeCredentialHandoff, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"path", "credential_path", "credential_file", "executable", "args", "env"} {
		call("connection_credentials_handoff", map[string]any{"id": authored.Connection.ID, field: credentialFile}, false)
	}
	call("credential_files", map[string]any{"path": credentialFile}, false)
	afterCredentialHandoff, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(beforeCredentialHandoff, afterCredentialHandoff) {
		t.Fatal("rejected credential-file handoff changed Node state")
	}
	call("connection_credentials_handoff", map[string]any{"id": authored.Connection.ID}, true)
	// Complete credential custody through the trusted host consumer, then
	// verify its public revision through MCP without receiving the material.
	const rotatedSecret = "synthetic-host-rotation-only"
	secretMaterials = append(secretMaterials, rotatedSecret)
	rotated, err := control.ReplaceConnectionCredential(t.Context(), authored.Connection.ID, authored.Connection.Revision, "host-credential-rotation", rotatedSecret, "TOKEN")
	if err != nil || json.Unmarshal(rotated.Connection["revision"], &authored.Connection.Revision) != nil {
		t.Fatal("host credential rotation did not acknowledge its public revision", err)
	}
	connectionRead := call("connection_get", map[string]any{"id": authored.Connection.ID}, true)
	if !strings.Contains(connectionRead.Content[0].(*sdk.TextContent).Text, authored.Connection.Revision) {
		t.Fatal("credential handoff did not verify the committed Connection revision")
	}
	call("connection_check", map[string]any{"id": authored.Connection.ID, "revision": authored.Connection.Revision}, true)
	call("mcp_catalog", map[string]any{"id": authored.Connection.ID}, true)
	call("workspace_service_set", map[string]any{"workspace": "agent-created", "service": "mcp", "enabled": true, "etag": etag()}, true)
	externalMount := map[string]any{"workspace": "agent-created", "alias": "docs", "connection_id": authored.Connection.ID, "builtin": "", "selected_tools": []string{"fixture"}, "dry_run": true}
	externalReviewResult := call("mcp_mount", externalMount, true)
	var externalReview workspaces.MCPMountPreview
	if err := json.Unmarshal([]byte(externalReviewResult.Content[0].(*sdk.TextContent).Text), &externalReview); err != nil {
		t.Fatal(err)
	}
	externalMount["dry_run"] = false
	externalMount["review"] = workspaces.MCPMountReviewRequest{WorkspaceETag: externalReview.Revision, Digest: externalReview.Digest, ConnectionRevision: externalReview.ConnectionRevision, AccessRevision: externalReview.AccessRevision, PublicationRevision: externalReview.PublicationRevision}
	call("mcp_mount", externalMount, true)
	call("mcp_mounts", map[string]any{"workspace": "agent-created"}, true)
	call("workspace_tools_list", map[string]any{"workspace": "agent-created"}, true)
	call("mcp_mount_delete", map[string]any{"workspace": "agent-created", "alias": "docs", "etag": etag()}, true)
	call("connection_delete", map[string]any{"id": authored.Connection.ID, "revision": authored.Connection.Revision}, true)

	request, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, daemon.BaseURL()+"/api/v1/workspaces", strings.NewReader(`{"slug":"forbidden"}`))
	request.Header.Set("Authorization", "Bearer "+credential.Token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatal("Workspace bearer reached direct Control")
	}
	call("workspace_create", map[string]any{"slug": "injected", "display_name": "", "retry_key": "injected", "dry_run": false, "shell": "touch /tmp/forged"}, false)
	// The mount review holds Access/publication owner locks; admission must
	// recheck committed state without recursively acquiring those same locks.
	mountArgs := map[string]any{"workspace": "operator-tools", "alias": "node", "connection_id": "", "builtin": "swobu-admin", "selected_tools": body["selected_tools"], "dry_run": true}
	reviewed := call("mcp_mount", mountArgs, true)
	var selfReview workspaces.MCPMountPreview
	if err := json.Unmarshal([]byte(reviewed.Content[0].(*sdk.TextContent).Text), &selfReview); err != nil {
		t.Fatal(err)
	}
	mountArgs["dry_run"] = false
	mountArgs["review"] = workspaces.MCPMountReviewRequest{WorkspaceETag: selfReview.Revision, Digest: selfReview.Digest, ConnectionRevision: selfReview.ConnectionRevision, AccessRevision: selfReview.AccessRevision, PublicationRevision: selfReview.PublicationRevision}
	call("mcp_mount", mountArgs, true)
	current, err := control.GetWorkspace(t.Context(), "operator-tools")
	if err != nil {
		t.Fatal(err)
	}
	call("mcp_mount_delete", map[string]any{"workspace": "operator-tools", "alias": "node", "etag": current.ManagementETag}, true)
	call("workspaces_list", map[string]any{}, false)
	if err := json.Unmarshal(put(true, nil), &preview); err != nil {
		t.Fatal(err)
	}
	put(false, &preview)
	principalArgs := map[string]any{"label": "Agent-managed identity", "retry_key": "mcp-principal"}
	createdPrincipalResult := call("principal_create", principalArgs, true)
	var createdPrincipal struct {
		Principal struct {
			ID       string `json:"id"`
			Revision string `json:"revision"`
		} `json:"principal"`
	}
	if err := json.Unmarshal([]byte(createdPrincipalResult.Content[0].(*sdk.TextContent).Text), &createdPrincipal); err != nil {
		t.Fatal(err)
	}
	call("principal_rename", map[string]any{"id": createdPrincipal.Principal.ID, "revision": createdPrincipal.Principal.Revision, "label": "Renamed via MCP"}, true)
	principalAudits := daemon.accessStore.PrincipalCreationAudits()
	found := false
	for _, audit := range principalAudits {
		if audit.Principal == createdPrincipal.Principal.ID {
			found = audit.Caller == principal.ID
		}
	}
	if !found {
		t.Fatal("MCP Principal creation lost admitted actor")
	}
	// Two independently admitted Principals race the same reviewed ETag.
	// The existing owner must acknowledge one write and reject the other.
	grant(createdPrincipal.Principal.ID, true)
	competingIntent, err := operatorclient.RestoreCredentialIssuanceIntent(createdPrincipal.Principal.ID, "operator-tools", "competing-admin-key")
	if err != nil {
		t.Fatal(err)
	}
	competingCredential, err := control.ApplyCredentialIssuance(t.Context(), competingIntent)
	if err != nil {
		t.Fatal(err)
	}
	secretMaterials = append(secretMaterials, competingCredential.Token)
	competing, err := sdk.NewClient(&sdk.Implementation{Name: "competing-admin-proof", Version: "1"}, nil).Connect(t.Context(), &sdk.StreamableClientTransport{Endpoint: daemon.BaseURL() + "/workspaces/operator-tools/mcp", HTTPClient: &http.Client{Transport: &bearerRoundTripper{next: http.DefaultTransport, token: competingCredential.Token}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer competing.Close()
	call("workspace_create", map[string]any{"slug": "race-reviewed", "display_name": "Before race", "retry_key": "race-create", "dry_run": false}, true)
	raceWorkspace, err := control.GetWorkspace(t.Context(), "race-reviewed")
	if err != nil {
		t.Fatal(err)
	}
	type raceOutcome struct {
		result *sdk.CallToolResult
		err    error
	}
	outcomes := make(chan raceOutcome, 2)
	for i, actor := range []*sdk.ClientSession{session, competing} {
		go func() {
			result, err := actor.CallTool(t.Context(), &sdk.CallToolParams{Name: "node__workspace_rename", Arguments: map[string]any{"workspace": "race-reviewed", "etag": raceWorkspace.ManagementETag, "display_name": []string{"First admin", "Second admin"}[i]}})
			outcomes <- raceOutcome{result, err}
		}()
	}
	writes := 0
	for range 2 {
		outcome := <-outcomes
		if outcome.err != nil || outcome.result == nil {
			t.Fatal("concurrent review proof lost transport", outcome.err)
		}
		if !outcome.result.IsError {
			writes++
		}
	}
	if writes != 1 {
		t.Fatal("competing reviewed writes did not admit exactly one effect")
	}
	raced, err := control.GetWorkspace(t.Context(), "race-reviewed")
	if err != nil || raced.ManagementRevision != raceWorkspace.ManagementRevision+1 || (raced.DisplayName != "First admin" && raced.DisplayName != "Second admin") {
		t.Fatal("concurrent write acknowledgements disagree with canonical readback", err)
	}

	// A remote SDK traverses the actual Owner multiplexer and application TLS.
	// Membership is fixture-published: this proves transport/admission and
	// self-unpublication, not production relay enrollment or DNS readiness.
	adminSlug, _ := routing.ParseWorkspaceSlug("operator-tools")
	if err := daemon.publicationStore.Set(adminSlug, true, daemon.publicationStore.Revision(), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	relayClient := ownerRelaySearchClient(t, daemon.publicHandler)
	relayClient.Transport = &bearerRoundTripper{next: relayClient.Transport, token: credential.Token}
	remote, err := sdk.NewClient(&sdk.Implementation{Name: "remote-admin-proof", Version: "1"}, nil).Connect(t.Context(), &sdk.StreamableClientTransport{Endpoint: "https://example.com/workspaces/operator-tools/mcp", HTTPClient: relayClient, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	remoteResult, err := remote.CallTool(t.Context(), &sdk.CallToolParams{Name: "node__workspace_create", Arguments: map[string]any{"slug": "relay-created", "display_name": "Relay Admin proof", "retry_key": "relay-create", "dry_run": false}})
	if err != nil || remoteResult.IsError {
		t.Fatal("remote Admin mutation failed")
	}
	if value, err := control.GetWorkspace(t.Context(), "relay-created"); err != nil || value.DisplayName != "Relay Admin proof" {
		t.Fatal("remote effect not visible in Node Control")
	}
	publicResult, err := remote.CallTool(t.Context(), &sdk.CallToolParams{Name: "node__workspace_publication_get", Arguments: map[string]any{"workspace": "operator-tools"}})
	if err != nil || publicResult.IsError {
		t.Fatal("remote publication read failed")
	}
	var publicSnapshot struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal([]byte(publicResult.Content[0].(*sdk.TextContent).Text), &publicSnapshot); err != nil {
		t.Fatal(err)
	}
	unpublished, err := remote.CallTool(t.Context(), &sdk.CallToolParams{Name: "node__workspace_publication_set", Arguments: map[string]any{"workspace": "operator-tools", "revision": publicSnapshot.Revision, "published": false}})
	if err != nil || unpublished.IsError {
		t.Fatal("self unpublish must acknowledge its admitted effect once")
	}
	afterUnpublish, err := remote.CallTool(t.Context(), &sdk.CallToolParams{Name: "node__workspaces_list", Arguments: map[string]any{}})
	if err == nil && afterUnpublish != nil && !afterUnpublish.IsError {
		t.Fatal("unpublished remote Admin retained authority")
	}
	call("workspaces_list", map[string]any{}, true) // local recovery does not need publication
	if _, err := control.NodeAuditEvents(t.Context()); err != nil {
		t.Fatalf("canonical audit reader rejected delegated actors: %v", err)
	}

	// Revoking this session's own credential completes once. Recover locally
	// using the sibling credential issued during the secure handoff above.
	revokeArgs := map[string]any{"principal": principal.ID, "credential": credential.Credential.ID, "dry_run": true, "revision": "", "retry_key": "self-credential-revoke"}
	revokeResult := call("credential_revoke", revokeArgs, true)
	var credentialReview struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal([]byte(revokeResult.Content[0].(*sdk.TextContent).Text), &credentialReview); err != nil {
		t.Fatal(err)
	}
	revokeArgs["dry_run"] = false
	revokeArgs["revision"] = credentialReview.Revision
	call("credential_revoke", revokeArgs, true)
	call("workspaces_list", map[string]any{}, false)
	_ = session.Close()
	session, err = client.Connect(t.Context(), &sdk.StreamableClientTransport{Endpoint: daemon.BaseURL() + "/workspaces/operator-tools/mcp", HTTPClient: &http.Client{Transport: &bearerRoundTripper{next: http.DefaultTransport, token: hostIssued.Token}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	call("workspaces_list", map[string]any{}, true)
	if _, err := control.NodeAuditEvents(t.Context()); err != nil {
		t.Fatalf("credential audit actor rejected: %v", err)
	}
	// This mutation revokes the same grant that admitted the current call.
	// It must finish once under Access's lock without reentering that lock.
	permissionArgs := map[string]any{"principal": principal.ID, "workspace": "operator-tools", "grant": false, "dry_run": true, "revision": "", "retry_key": "self-revoke"}
	permissionResult := call("permission_set", permissionArgs, true)
	var permissionPreview struct {
		Revision string `json:"revision"`
	}
	if err := json.Unmarshal([]byte(permissionResult.Content[0].(*sdk.TextContent).Text), &permissionPreview); err != nil {
		t.Fatal(err)
	}
	permissionArgs["dry_run"] = false
	permissionArgs["revision"] = permissionPreview.Revision
	call("permission_set", permissionArgs, true)
	args["slug"] = "after-revocation"
	args["retry_key"] = "after-revocation"
	call("workspace_create", args, false)
	deniedSlug, _ := routing.ParseWorkspaceSlug("after-revocation")
	if _, err := daemon.configStore.GetWorkspace(t.Context(), deniedSlug); err == nil {
		t.Fatal("revoked call published effect")
	}
	if err := daemon.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := Start(t.Context(), StartInput{ConfigPath: path, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	saved, err := restarted.configStore.GetWorkspace(t.Context(), slug)
	if err != nil || saved.DisplayName() != "Renamed through Workspace MCP" {
		t.Fatalf("restart lost committed state: %#v %v", saved, err)
	}
	// Removing the Admin-bearing Workspace must leave ordinary host recovery
	// available. Its replacement is a fresh empty Workspace, not a rescue kind.
	recovery := operatorclient.New(http.DefaultClient, restarted.BaseURL())
	deletion, err := recovery.PreviewWorkspaceDeletion(t.Context(), "operator-tools")
	if err != nil {
		t.Fatal(err)
	}
	if err := recovery.ApplyWorkspaceDeletion(t.Context(), "operator-tools", deletion.ManagementETag, "remove-admin-workspace"); err != nil {
		t.Fatal(err)
	}
	recreate, err := operatorclient.NewWorkspaceCreationIntent(workspaces.CreateWorkspace{Slug: "operator-tools"}, "recreate-admin-workspace")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := recovery.ApplyWorkspaceCreation(t.Context(), recreate); err != nil {
		t.Fatal("host could not recreate the ordinary Workspace", err)
	}
	if replacement, err := recovery.GetWorkspace(t.Context(), "operator-tools"); err != nil || len(replacement.EnabledServices) != 0 {
		t.Fatal("recreated Workspace acquired special authority", err)
	}
}

// Proves the ordinary swobu-admin mount shares tool-call admission.
func TestWorkspaceOperationalMCPQuotaWithExplicitAdminSelection(t *testing.T) {
	t.Setenv("SWOBU_LEDGER", "sqlite")
	t.Setenv("SWOBU_ADMIN_CHILD", "1")
	t.Setenv("SWOBU_ADMIN_SOURCE_TOKEN", "synthetic-source-only")
	root := t.TempDir()
	t.Setenv(config.EnvSwobuHome, root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "clients"))
	if err := os.MkdirAll(filepath.Join(root, "clients", "opencode"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "clients", "opencode", "opencode.json"), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "swobu.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 5\nworkspaces:\n  operator-tools:\n    enabled_services: [mcp]\n    routes: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	daemon, err := Start(t.Context(), StartInput{ConfigPath: path, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	control := operatorclient.New(http.DefaultClient, daemon.BaseURL())
	principal, _, err := control.CreatePrincipal(t.Context(), "Delegated operator", "create-admin-principal")
	if err != nil {
		t.Fatal(err)
	}
	grant := func(id string, allow bool) {
		t.Helper()
		preview, err := control.PreviewWorkspacePermission(t.Context(), "operator-tools", id, allow, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		review, err := operatorclient.ReviewPermission(preview, map[bool]string{true: "grant-admin-", false: "revoke-admin-"}[allow]+id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := control.ApplyWorkspacePermission(t.Context(), review); err != nil {
			t.Fatal(err)
		}
	}
	grant(principal.ID, true)
	credentialIntent, err := operatorclient.RestoreCredentialIssuanceIntent(principal.ID, "operator-tools", "admin-key")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := control.ApplyCredentialIssuance(t.Context(), credentialIntent)
	if err != nil {
		t.Fatal(err)
	}
	// Mount through ordinary local Control preview/apply. A broad existing grant
	// is visible in that review; builtin authority is never selected by alias.
	selected := []string{"workspace_get", "workspace_service_limits_set"}
	body := map[string]any{"builtin": "swobu-admin", "selected_tools": selected}
	put := func(dryRun bool, review *workspaces.MCPMountPreview) []byte {
		t.Helper()
		if review == nil {
			delete(body, "review")
		}
		if review != nil {
			body["review"] = workspaces.MCPMountReviewRequest{WorkspaceETag: review.Revision, Digest: review.Digest, ConnectionRevision: review.ConnectionRevision, AccessRevision: review.AccessRevision, PublicationRevision: review.PublicationRevision}
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPut, daemon.BaseURL()+"/api/v1/workspaces/operator-tools/mcp-mounts/node?dry_run="+map[bool]string{true: "true", false: "false"}[dryRun], bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		if review != nil {
			request.Header.Set("If-Match", `"`+review.Revision+`"`)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		result, _ := io.ReadAll(response.Body)
		if response.StatusCode != 200 {
			t.Fatalf("mount status %d: %s", response.StatusCode, result)
		}
		return result
	}
	var preview workspaces.MCPMountPreview
	if err := json.Unmarshal(put(true, nil), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Builtin != routing.AdminMCPSource || !preview.IdentityVerified || len(preview.AffectedPrincipals) != 1 || preview.AffectedPrincipals[0].ID != principal.ID {
		t.Fatalf("builtin delegation missing from review: %#v", preview)
	}
	put(false, &preview)
	client := sdk.NewClient(&sdk.Implementation{Name: "admin-parity-proof", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &sdk.StreamableClientTransport{Endpoint: daemon.BaseURL() + "/workspaces/operator-tools/mcp", HTTPClient: &http.Client{Transport: &bearerRoundTripper{next: http.DefaultTransport, token: credential.Token}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	catalog, err := session.ListTools(t.Context(), nil)
	if err != nil || len(catalog.Tools) != len(selected) {
		t.Fatalf("catalog %#v: %v", catalog, err)
	}
	current, err := control.GetWorkspace(t.Context(), "operator-tools")
	if err != nil {
		t.Fatal(err)
	}
	set, err := session.CallTool(t.Context(), &sdk.CallToolParams{Name: "node__workspace_service_limits_set", Arguments: map[string]any{
		"workspace": "operator-tools", "etag": current.ManagementETag,
		"limits": map[string]any{"mcp": map[string]any{"requests_per_minute": 1}},
	}})
	if err != nil || set == nil || set.IsError {
		t.Fatalf("selected admin mutation failed: %#v %v", set, err)
	}
	read, err := control.GetWorkspace(t.Context(), "operator-tools")
	if err != nil || read.ServiceLimits.MCP.RequestsPerMinute != 1 {
		t.Fatalf("admin policy not published: %#v %v", read, err)
	}
	call := func(allowed bool) {
		t.Helper()
		result, err := session.CallTool(t.Context(), &sdk.CallToolParams{Name: "node__workspace_get", Arguments: map[string]any{"workspace": "operator-tools"}})
		if allowed && (err != nil || result == nil || result.IsError) {
			t.Fatalf("first tool failed: %#v %v", result, err)
		}
		if !allowed && err == nil && result != nil && !result.IsError {
			t.Fatal("exhausted MCP total allowed admin tool")
		}
	}
	call(true)
	call(false)
	// Discovery remains free, including a fresh SDK initialization after the
	// bucket has been exhausted by an ordinary mounted builtin tool.
	fresh, err := client.Connect(t.Context(), &sdk.StreamableClientTransport{Endpoint: daemon.BaseURL() + "/workspaces/operator-tools/mcp", HTTPClient: &http.Client{Transport: &bearerRoundTripper{next: http.DefaultTransport, token: credential.Token}}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal("quota blocked initialize:", err)
	}
	defer fresh.Close()
	catalog, err = fresh.ListTools(t.Context(), nil)
	if err != nil || len(catalog.Tools) != 2 {
		t.Fatalf("quota blocked discovery or widened selection: %#v %v", catalog, err)
	}
	result, err := fresh.CallTool(t.Context(), &sdk.CallToolParams{Name: "node__workspace_get", Arguments: map[string]any{"workspace": "operator-tools"}})
	if err == nil && result != nil && !result.IsError {
		t.Fatal("new SDK session bypassed Workspace quota")
	}
}
