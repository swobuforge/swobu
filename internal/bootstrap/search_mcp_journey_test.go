package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/swobuforge/swobu/internal/accessstate"
	evidenceStore "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/search"
)

func TestSearchAndMCPWorkspaceJourneyNeedsNoLLMRoute(t *testing.T) {
	t.Setenv("SWOBU_LEDGER", "sqlite")
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "swobu.yaml")
	configYAML := `schema_version: 5
workspaces:
  research:
    routes: []
    search:
      connection_id: cn_fixture_search
    mcp_mounts:
      docs:
        connection_id: cn_fixture_docs
        allowed_tools: [search]
      tickets:
        connection_id: cn_fixture_tickets
        allowed_tools: [search]
`
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	// The journey also issues a credential for the second Workspace; admit it
	// in the same Node snapshot before creating that Principal.
	configYAML += "  other:\n    routes: []\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	publishMCPFixtureConnection(t, configPath, "cn_fixture_docs")
	publishMCPFixtureConnection(t, configPath, "cn_fixture_tickets")
	publishSearchFixtureConnection(t, configPath, "cn_fixture_search")

	object, err := canonical.ParseJSONObject([]byte(`{"type":"object","properties":{"query":{"type":"string"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	toolKey, _ := canonical.NewToolKey("mcp/docs", canonical.ToolKindFunction, "search")
	declaration, err := canonical.NewFunctionTool(toolKey, "Search docs", canonical.NewToolSchemaObject(object), canonical.SchemaContract{Profile: canonical.SchemaProfileUnprofiled})
	if err != nil {
		t.Fatal(err)
	}
	sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "docs")
	mcpDeclaration, err := canonical.NewMCPToolSource(sourceKey, "", canonical.MCPSource{}, []canonical.ToolDeclaration{declaration})
	if err != nil {
		t.Fatal(err)
	}
	source, _ := mcpDeclaration.MCP()
	var docsCalls, ticketCalls atomic.Int32
	blockingStarted := make(chan struct{})
	blockingRelease := make(chan struct{})
	defer close(blockingRelease)
	lateSuccessStarted := make(chan struct{})

	daemon, err := Start(t.Context(), StartInput{
		ConfigPath:    configPath,
		StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"},
		SearchService: search.Service{Sources: map[string]search.Source{
			"research": search.StaticSource{{URL: "https://docs.example.test", Title: "Docs", Snippet: "workspace result"}},
		}, ConnectionIDs: func(string) ([]string, bool) { return []string{"research"}, true }},
		MCPConnectionBindings: map[string]mcp.SourceBinding{
			"cn_fixture_docs": {
				ID: "cn_fixture_docs", ConnectionID: "cn_fixture_docs", ConnectionRevision: "1", Source: source,
				Call: func(callCtx context.Context, name string, args json.RawMessage) (*sdk.CallToolResult, error) {
					docsCalls.Add(1)
					if name == "search" && string(args) == `{"query":"success-after-cancel"}` {
						close(lateSuccessStarted)
						select {
						case <-callCtx.Done():
							return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "private completed effect"}}}, nil
						case <-blockingRelease:
							return nil, errors.New("blocking source released")
						}
					}
					if name == "search" && string(args) == `{"query":"blocking"}` {
						close(blockingStarted)
						select {
						case <-callCtx.Done():
							return nil, callCtx.Err()
						case <-blockingRelease:
							return nil, errors.New("blocking source released")
						}
					}
					if name == "search" && string(args) == `{"query":"fail-source"}` {
						return nil, errors.New("docs source unavailable")
					}
					if name == "search" {
						switch string(args) {
						case `{"query":"deadline"}`:
							return nil, context.DeadlineExceeded
						case `{"query":"cancelled"}`:
							return nil, context.Canceled
						case `{"query":"empty-result"}`:
							return nil, nil
						case `{"query":"tool-error"}`:
							return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: "private native tool error"}}}, nil
						}
					}
					if name != "search" || string(args) != `{"query":"workspace"}` {
						return nil, os.ErrInvalid
					}
					return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "MCP workspace result"}}}, nil
				},
			},
			"cn_fixture_tickets": {
				ID: "cn_fixture_tickets", ConnectionID: "cn_fixture_tickets", ConnectionRevision: "1", Source: source,
				Call: func(_ context.Context, name string, args json.RawMessage) (*sdk.CallToolResult, error) {
					ticketCalls.Add(1)
					if name != "search" || string(args) != `{"query":"workspace"}` {
						return nil, os.ErrInvalid
					}
					return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "Ticket workspace result"}}}, nil
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()

	research, err := routing.ParseWorkspaceSlug("research")
	if err != nil {
		t.Fatal(err)
	}
	principal, credential, err := daemon.accessStore.Create("journey", research)
	if err != nil {
		t.Fatal(err)
	}
	otherWorkspace, err := routing.ParseWorkspaceSlug("other")
	if err != nil {
		t.Fatal(err)
	}
	_, otherCredential, err := daemon.accessStore.Create("other-journey", otherWorkspace)
	if err != nil {
		t.Fatal(err)
	}

	for _, testCase := range []struct {
		name   string
		bearer string
		origin string
		status int
	}{
		{name: "forged audience", bearer: "swak_forged_workspace_token", status: http.StatusUnauthorized},
		{name: "valid wrong audience", bearer: otherCredential.Token, status: http.StatusNotFound},
		{name: "hostile origin", bearer: credential.Token, origin: "https://attacker.example", status: http.StatusForbidden},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodPost, daemon.BaseURL()+"/workspaces/research/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer "+testCase.bearer)
			request.Header.Set("Content-Type", "application/json")
			if testCase.origin != "" {
				request.Header.Set("Origin", testCase.origin)
			}
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode != testCase.status {
				t.Fatalf("status=%d, want %d", response.StatusCode, testCase.status)
			}
		})
	}

	// Search has an independent operation ceiling; Models and MCP stay unlimited.
	revision, err := daemon.accessStore.PermissionRevision(principal.ID, research)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.accessStore.ApplyPermissionServicesWithRatesChecked(t.Context(), principal.ID, research, revision, "search-rate", func() error { return nil }, nil, []accessstate.RequestRateLimit{{Service: routing.ServiceSearch, RequestsPerMinute: 1}}); err != nil {
		t.Fatal(err)
	}
	searchRequest, err := http.NewRequest(http.MethodPost, daemon.BaseURL()+"/workspaces/research/search", strings.NewReader(`{"query":"workspace","limit":1}`))
	if err != nil {
		t.Fatal(err)
	}
	searchRequest.Header.Set("Authorization", "Bearer "+credential.Token)
	searchRequest.Header.Set("Content-Type", "application/json")
	searchResponse, err := http.DefaultClient.Do(searchRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer searchResponse.Body.Close()
	searchBody, err := io.ReadAll(searchResponse.Body)
	if err != nil {
		t.Fatal(err)
	}
	if searchResponse.StatusCode != http.StatusOK || !strings.Contains(string(searchBody), "docs.example.test") {
		t.Fatalf("search status=%d body=%s", searchResponse.StatusCode, searchBody)
	}
	deniedSearch, err := http.NewRequest(http.MethodPost, daemon.BaseURL()+"/workspaces/research/search", strings.NewReader(`{"query":"workspace","limit":1}`))
	if err != nil {
		t.Fatal(err)
	}
	deniedSearch.Header = searchRequest.Header.Clone()
	deniedResponse, err := http.DefaultClient.Do(deniedSearch)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, deniedResponse.Body)
	_ = deniedResponse.Body.Close()
	if deniedResponse.StatusCode != 429 || deniedResponse.Header.Get("Retry-After") == "" || deniedResponse.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("Search quota status=%d headers=%v", deniedResponse.StatusCode, deniedResponse.Header)
	}
	// The existing two-event assertion below proves denial never starts Search.
	evidenceResponse, err := http.Get(daemon.BaseURL() + "/api/v1/search-evidence")
	if err != nil {
		t.Fatal(err)
	}
	evidenceBody, err := io.ReadAll(evidenceResponse.Body)
	_ = evidenceResponse.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	var evidence struct {
		Events []struct {
			Workspace string `json:"Workspace"`
			Source    string `json:"Source"`
			Outcome   string `json:"Outcome"`
		} `json:"events"`
	}
	if err := json.Unmarshal(evidenceBody, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidenceResponse.StatusCode != http.StatusOK || len(evidence.Events) != 2 || evidence.Events[0].Workspace != "research" || evidence.Events[0].Source != "research" || evidence.Events[1].Outcome != "succeeded" || strings.Contains(string(evidenceBody), "workspace") || strings.Contains(string(evidenceBody), "query") {
		t.Fatalf("search evidence status=%d body=%s", evidenceResponse.StatusCode, evidenceBody)
	}

	llmRequest, err := http.NewRequest(http.MethodPost, daemon.BaseURL()+"/workspaces/research/v1/responses", strings.NewReader(`{"model":"fixture","input":"must not route","stream":false}`))
	if err != nil {
		t.Fatal(err)
	}
	llmRequest.Header.Set("Authorization", "Bearer "+credential.Token)
	llmRequest.Header.Set("Content-Type", "application/json")
	llmResponse, err := http.DefaultClient.Do(llmRequest)
	if err != nil {
		t.Fatal(err)
	}
	llmBody, err := io.ReadAll(llmResponse.Body)
	_ = llmResponse.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if llmResponse.StatusCode != http.StatusBadRequest || !strings.Contains(string(llmBody), "OPERATION_NOT_CONFIGURED") {
		t.Fatalf("LLM-not-configured status=%d body=%s", llmResponse.StatusCode, llmBody)
	}

	transport := &bearerRoundTripper{next: http.DefaultTransport, token: credential.Token}
	client := sdk.NewClient(&sdk.Implementation{Name: "journey-client", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), &sdk.StreamableClientTransport{
		Endpoint:             daemon.BaseURL() + "/workspaces/research/mcp",
		HTTPClient:           &http.Client{Transport: transport},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range listed.Tools {
		names[tool.Name] = true
	}
	if len(listed.Tools) != 2 || !names["docs__search"] || !names["tickets__search"] {
		t.Fatalf("tools=%#v err=%v", listed, err)
	}
	result, err := session.CallTool(t.Context(), &sdk.CallToolParams{Name: "docs__search", Arguments: map[string]any{"query": "workspace"}})
	if err != nil || len(result.Content) != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	text, ok := result.Content[0].(*sdk.TextContent)
	if !ok || text.Text != "MCP workspace result" {
		t.Fatalf("MCP result=%#v", result.Content)
	}
	if docsCalls.Load() != 1 || ticketCalls.Load() != 0 {
		t.Fatalf("docs dispatch crossed source bindings: docs=%d tickets=%d", docsCalls.Load(), ticketCalls.Load())
	}
	ticketResult, err := session.CallTool(t.Context(), &sdk.CallToolParams{Name: "tickets__search", Arguments: map[string]any{"query": "workspace"}})
	if err != nil || len(ticketResult.Content) != 1 {
		t.Fatalf("ticket result=%#v err=%v", ticketResult, err)
	}
	ticketText, ok := ticketResult.Content[0].(*sdk.TextContent)
	if !ok || ticketText.Text != "Ticket workspace result" {
		t.Fatalf("ticket result=%#v", ticketResult.Content)
	}
	if _, err := session.CallTool(t.Context(), &sdk.CallToolParams{Name: "swobu__manage_workspace", Arguments: map[string]any{"slug": "research"}}); err == nil {
		t.Fatal("reserved management tool was callable through Workspace MCP")
	}
	if docsCalls.Load() != 1 || ticketCalls.Load() != 1 {
		t.Fatalf("expected one call per source and no management dispatch: docs=%d tickets=%d", docsCalls.Load(), ticketCalls.Load())
	}
	if _, err := session.CallTool(t.Context(), &sdk.CallToolParams{Name: "missing__search", Arguments: map[string]any{"query": "workspace"}}); err == nil {
		t.Fatal("unknown tool was callable through Workspace MCP")
	}
	failed, err := session.CallTool(t.Context(), &sdk.CallToolParams{Name: "docs__search", Arguments: map[string]any{"query": "fail-source"}})
	if err == nil && (failed == nil || !failed.IsError) {
		t.Fatalf("failed source fabricated success: %#v", failed)
	}
	if docsCalls.Load() != 2 || ticketCalls.Load() != 1 {
		t.Fatalf("source failure retried or crossed bindings: docs=%d tickets=%d", docsCalls.Load(), ticketCalls.Load())
	}
	for _, query := range []string{"deadline", "cancelled", "empty-result", "tool-error"} {
		result, err := session.CallTool(t.Context(), &sdk.CallToolParams{Name: "docs__search", Arguments: map[string]any{"query": query}})
		if err == nil && (result == nil || !result.IsError) {
			t.Fatalf("%s fabricated success: %#v", query, result)
		}
	}
	if docsCalls.Load() != 6 || ticketCalls.Load() != 1 {
		t.Fatalf("failure matrix retried or crossed bindings: docs=%d tickets=%d", docsCalls.Load(), ticketCalls.Load())
	}
	callContext, cancelCall := context.WithCancel(t.Context())
	defer cancelCall()
	callDone := make(chan error, 1)
	go func() {
		_, err := session.CallTool(callContext, &sdk.CallToolParams{Name: "docs__search", Arguments: map[string]any{"query": "blocking"}})
		callDone <- err
	}()
	select {
	case <-blockingStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("source call never started")
	}
	active := daemon.executionEvidenceStore.ProjectActivity(evidenceStore.ProjectionInput{Scope: evidenceStore.ProjectionScope{Kind: evidenceStore.ProjectionScopeWorkspace, Workspace: "research"}})
	pendingIdentity := ""
	for _, operation := range active.MCPOperations {
		if operation.SettledAt.IsZero() {
			if pendingIdentity != "" || operation.Outcome != "" || operation.Occurrence != "" || operation.Source != "cn_fixture_docs" {
				t.Fatalf("invalid active operation=%#v", operation)
			}
			pendingIdentity = operation.OperationID
		}
	}
	if pendingIdentity == "" {
		t.Fatal("in-flight source call has no standalone Activity record")
	}
	activeResponse, err := http.Get(daemon.BaseURL() + "/api/v1/activity?workspace=research")
	if err != nil {
		t.Fatal(err)
	}
	activeBody, err := io.ReadAll(activeResponse.Body)
	_ = activeResponse.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	var activeWire struct {
		Operations []map[string]json.RawMessage `json:"mcp_operations"`
	}
	if err := json.Unmarshal(activeBody, &activeWire); err != nil {
		t.Fatal(err)
	}
	if activeResponse.StatusCode != http.StatusOK || len(activeWire.Operations) != 8 {
		t.Fatalf("active Activity response status=%d body=%s", activeResponse.StatusCode, activeBody)
	}
	activeFound := false
	for _, operation := range activeWire.Operations {
		var identity string
		if err := json.Unmarshal(operation["operation_id"], &identity); err != nil {
			t.Fatal(err)
		}
		if identity == pendingIdentity {
			activeFound = true
			for _, field := range []string{"settled_at", "outcome", "execution_possibility"} {
				if _, exists := operation[field]; exists {
					t.Fatalf("active operation advertises terminal field %s: %s", field, activeBody)
				}
			}
		}
	}
	if !activeFound {
		t.Fatal("canonical Activity omitted the in-flight operation")
	}
	cancelCall()
	select {
	case err := <-callDone:
		if err == nil {
			t.Fatal("cancelled client call reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("client call did not cancel")
	}
	settlementDeadline := time.NewTimer(5 * time.Second)
	defer settlementDeadline.Stop()
	settlementPoll := time.NewTicker(10 * time.Millisecond)
	defer settlementPoll.Stop()
	settled := false
	for !settled {
		for _, operation := range daemon.executionEvidenceStore.ProjectActivity(evidenceStore.ProjectionInput{}).MCPOperations {
			if operation.OperationID == pendingIdentity && !operation.SettledAt.IsZero() {
				if operation.Outcome != execution.Cancelled || operation.Occurrence != execution.MayHaveOccurred {
					t.Fatalf("cancelled source settlement=%#v", operation)
				}
				settled = true
			}
		}
		if !settled {
			select {
			case <-settlementPoll.C:
			case <-settlementDeadline.C:
				t.Fatal("source cancellation left an unsettled Activity operation")
			}
		}
	}
	lateContext, cancelLate := context.WithCancel(t.Context())
	defer cancelLate()
	lateDone := make(chan error, 1)
	go func() {
		_, err := session.CallTool(lateContext, &sdk.CallToolParams{Name: "docs__search", Arguments: map[string]any{"query": "success-after-cancel"}})
		lateDone <- err
	}()
	select {
	case <-lateSuccessStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("late-success source call never started")
	}
	lateIdentity := ""
	for _, operation := range daemon.executionEvidenceStore.ProjectActivity(evidenceStore.ProjectionInput{}).MCPOperations {
		if operation.SettledAt.IsZero() {
			if lateIdentity != "" {
				t.Fatal("multiple unexpected active operations")
			}
			lateIdentity = operation.OperationID
		}
	}
	if lateIdentity == "" {
		t.Fatal("late-success call has no active evidence")
	}
	cancelLate()
	select {
	case err := <-lateDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("client cancellation=%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("late-success client call did not cancel")
	}
	lateDeadline := time.NewTimer(5 * time.Second)
	defer lateDeadline.Stop()
	latePoll := time.NewTicker(10 * time.Millisecond)
	defer latePoll.Stop()
	lateSettled := false
	for !lateSettled {
		for _, operation := range daemon.executionEvidenceStore.ProjectActivity(evidenceStore.ProjectionInput{}).MCPOperations {
			if operation.OperationID == lateIdentity && !operation.SettledAt.IsZero() {
				if operation.Outcome != execution.Succeeded || operation.Occurrence != execution.Occurred {
					t.Fatalf("client cancellation rewrote successful source execution: %#v", operation)
				}
				lateSettled = true
			}
		}
		if !lateSettled {
			select {
			case <-latePoll.C:
			case <-lateDeadline.C:
				t.Fatal("late-success source never settled")
			}
		}
	}
	if revoked, err := daemon.accessStore.RevokeCredential(principal.ID, credential.Credential.ID); err != nil || !revoked {
		t.Fatalf("credential revocation=%v err=%v", revoked, err)
	}
	if _, err := session.CallTool(t.Context(), &sdk.CallToolParams{Name: "docs__search", Arguments: map[string]any{"query": "workspace"}}); err == nil {
		t.Fatal("revoked credential retained MCP session authority")
	}
	if docsCalls.Load() != 8 || ticketCalls.Load() != 1 {
		t.Fatal("revoked credential dispatched a source call")
	}
	activityResponse, err := http.Get(daemon.BaseURL() + "/api/v1/activity?workspace=research")
	if err != nil {
		t.Fatal(err)
	}
	activityBody, err := io.ReadAll(activityResponse.Body)
	_ = activityResponse.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	var activity evidenceStore.ActivityProjection
	if err := json.Unmarshal(activityBody, &activity); err != nil {
		t.Fatal(err)
	}
	if activityResponse.StatusCode != http.StatusOK || len(activity.Exchanges) != 0 || len(activity.MCPOperations) != 9 {
		t.Fatalf("direct MCP activity=%s", activityBody)
	}
	sources := map[string]int{}
	identities := map[string]bool{}
	uncertain := 0
	cancelled, toolErrors := 0, 0
	for _, operation := range activity.MCPOperations {
		if operation.AccessID != principal.ID.String() || operation.Workspace != "research" || operation.Tool != "search" || operation.OperationID == "" || identities[operation.OperationID] || operation.SettledAt.Before(operation.StartedAt) {
			t.Fatalf("direct MCP operation=%#v", operation)
		}
		identities[operation.OperationID] = true
		sources[operation.Source]++
		if operation.Outcome == execution.Failed && operation.Occurrence == execution.MayHaveOccurred {
			uncertain++
		} else if operation.Outcome == execution.Cancelled && operation.Occurrence == execution.MayHaveOccurred {
			cancelled++
		} else if operation.Outcome == execution.Failed && operation.Occurrence == execution.Occurred {
			toolErrors++
		} else if operation.Outcome != execution.Succeeded || operation.Occurrence != execution.Occurred {
			t.Fatalf("operation settlement=%#v", operation)
		}
	}
	if sources["cn_fixture_docs"] != 8 || sources["cn_fixture_tickets"] != 1 || uncertain != 3 || cancelled != 2 || toolErrors != 1 {
		t.Fatalf("source evidence=%v uncertain=%d cancelled=%d tool errors=%d", sources, uncertain, cancelled, toolErrors)
	}
	for _, private := range []string{credential.Token, "fail-source", "MCP workspace result", "Ticket workspace result", "docs source unavailable", "private native tool error", "private completed effect", "success-after-cancel", `"query"`} {
		if strings.Contains(string(activityBody), private) {
			t.Fatalf("Activity leaked private content %q", private)
		}
	}
	// Exercise the same canonical Activity resource read by the Web Operator.
	// The exact terminal state and source identities are already checked above.
	if len(activity.MCPOperations) != 9 {
		t.Fatalf("Activity lost standalone operations: %#v", activity)
	}
	byIdentity := map[string]evidenceStore.MCPOperation{}
	for _, operation := range activity.MCPOperations {
		byIdentity[operation.OperationID] = operation
	}
	if _, exists := byIdentity[lateIdentity]; !exists {
		t.Fatalf("Activity lost late operation %s", lateIdentity)
	}
	otherResponse, err := http.Get(daemon.BaseURL() + "/api/v1/activity?workspace=other")
	if err != nil {
		t.Fatal(err)
	}
	var other evidenceStore.ActivityProjection
	if err := json.NewDecoder(otherResponse.Body).Decode(&other); err != nil {
		t.Fatal(err)
	}
	_ = otherResponse.Body.Close()
	if otherResponse.StatusCode != http.StatusOK || len(other.MCPOperations) != 0 || len(other.Exchanges) != 0 {
		t.Fatalf("Workspace Activity leaked evidence: %#v", other)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	if err := daemon.Close(); err != nil {
		t.Fatal(err)
	}
	if health := daemon.ledgerWriter.Health(); health.CommittedMCPOperationsTotal != 9 || health.DroppedMCPOperationsTotal != 0 || health.CommittedExchangesTotal != 0 {
		t.Fatalf("SDK capture health=%#v", health)
	}
	restarted, err := Start(t.Context(), StartInput{ConfigPath: configPath, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	response, err := http.Get(restarted.BaseURL() + "/api/v1/activity?workspace=research")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var restored evidenceStore.ActivityProjection
	if err := json.NewDecoder(response.Body).Decode(&restored); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || len(restored.Exchanges) != 0 || len(restored.MCPOperations) != 9 {
		t.Fatalf("restarted Activity=%#v status=%d", restored, response.StatusCode)
	}
	for _, operation := range restored.MCPOperations {
		if original, exists := byIdentity[operation.OperationID]; !exists || operation != original {
			t.Fatalf("restart changed SDK source truth: %#v", operation)
		}
	}
	if len(restarted.executionEvidenceStore.ProjectActivity(evidenceStore.ProjectionInput{}).MCPOperations) != 0 {
		t.Fatal("restart proof used restored memory instead of Ledger query")
	}
	otherResponse, err = http.Get(restarted.BaseURL() + "/api/v1/activity?workspace=other")
	if err != nil {
		t.Fatal(err)
	}
	var restartedOther evidenceStore.ActivityProjection
	if err := json.NewDecoder(otherResponse.Body).Decode(&restartedOther); err != nil {
		t.Fatal(err)
	}
	_ = otherResponse.Body.Close()
	if otherResponse.StatusCode != http.StatusOK || len(restartedOther.MCPOperations) != 0 || len(restartedOther.Exchanges) != 0 {
		t.Fatalf("restart leaked Workspace history: %#v", restartedOther)
	}
}

type bearerRoundTripper struct {
	next  http.RoundTripper
	token string
}

func (t *bearerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return t.next.RoundTrip(clone)
}
