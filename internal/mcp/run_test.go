package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestRunClassifiesMCPCallsByNamespaceAndRejectsMixedBatchBeforeBudget(t *testing.T) {
	request, remoteKey, localKey := runtimeTestRequest(t)
	run := runtimeTestRun(t, request, remoteKey)
	remoteCall := runtimeTestCall(t, "call_remote", remoteKey)
	localCall := runtimeTestCall(t, "call_local", localKey)

	remoteResponse := runtimeTestResponse(t, remoteCall)
	calls, err := run.Calls(remoteResponse)
	if err != nil || len(calls) != 1 || run.callCount != 0 || run.roundCount != 0 {
		t.Fatalf("remote classification = %#v count=%d/%d err=%v", calls, run.callCount, run.roundCount, err)
	}
	repeated, err := run.Calls(remoteResponse)
	if err != nil || len(repeated) != 1 || run.callCount != 0 || run.roundCount != 0 {
		t.Fatalf("repeated classification = %#v count=%d/%d err=%v", repeated, run.callCount, run.roundCount, err)
	}

	beforeCalls, beforeRounds := run.callCount, run.roundCount
	mixedResponse := runtimeTestResponse(t, remoteCall, localCall)
	_, err = run.Calls(mixedResponse)
	var canonicalError canonical.Error
	if !errors.As(err, &canonicalError) || canonicalError.Code != canonical.ErrorCodeNotImplemented {
		t.Fatalf("mixed classification error = %T %v", err, err)
	}
	if run.callCount != beforeCalls || run.roundCount != beforeRounds {
		t.Fatalf("mixed batch consumed budget: %d/%d", run.callCount, run.roundCount)
	}
}

func TestRunClassifiesOnlyUnresolvedCallerWorkAsMixedWithMCP(t *testing.T) {
	request, remoteKey, localKey := runtimeTestRequest(t)
	run := runtimeTestRun(t, request, remoteKey)
	remoteCall := runtimeTestCall(t, "call_remote", remoteKey)
	webCall, webResult := runtimeTestWebSearchLifecycle(t, "call_web")
	providerDiscoveryCall, providerDiscoveryResult := runtimeTestDiscoveryLifecycle(
		t, "call_provider_discovery", canonical.DiscoveryExecutorProvider,
	)
	clientDiscoveryCall, _ := runtimeTestDiscoveryLifecycle(
		t, "call_client_discovery", canonical.DiscoveryExecutorClient,
	)
	customKey, _ := canonical.NewRequestToolKey(canonical.ToolKindCustom, "shell")
	customCall := runtimeTestTextCall(t, "call_custom", customKey)

	tests := []struct {
		name      string
		items     []canonical.CanonicalItem
		wantMixed bool
	}{
		{name: "web search", items: []canonical.CanonicalItem{remoteCall, webCall, webResult}},
		{name: "provider discovery", items: []canonical.CanonicalItem{remoteCall, providerDiscoveryCall, providerDiscoveryResult}},
		{name: "function", items: []canonical.CanonicalItem{remoteCall, runtimeTestCall(t, "call_function", localKey)}, wantMixed: true},
		{name: "custom", items: []canonical.CanonicalItem{remoteCall, customCall}, wantMixed: true},
		{name: "client discovery", items: []canonical.CanonicalItem{remoteCall, clientDiscoveryCall}, wantMixed: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls, err := run.Calls(runtimeTestResponse(t, test.items...))
			var canonicalError canonical.Error
			mixed := errors.As(err, &canonicalError) &&
				canonicalError.Code == canonical.ErrorCodeNotImplemented
			if mixed != test.wantMixed {
				t.Fatalf("mixed = %v, calls = %#v, err = %T %v", mixed, calls, err, err)
			}
			if !test.wantMixed && len(calls) != 1 {
				t.Fatalf("MCP calls = %#v", calls)
			}
		})
	}
}

func TestBindingsRetainOnlyLinearPerToolIdentityAtCatalogLimit(t *testing.T) {
	sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindNamespace, "docs")
	source, _ := canonical.NewMCPSource(
		"https://mcp.example.test/rpc", canonical.Unspecified[[]string](),
	)
	schemaObject, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	schema := canonical.NewToolSchemaObject(schemaObject)
	tools := make([]canonical.ToolDeclaration, 0, MaxToolsPerSource)
	for index := 0; index < MaxToolsPerSource; index++ {
		key, _ := canonical.NewToolKey(
			"mcp/docs", canonical.ToolKindFunction, fmt.Sprintf("tool_%03d", index),
		)
		declaration, err := canonical.NewFunctionTool(
			key, "", schema, canonical.Unspecified[bool](),
		)
		if err != nil {
			t.Fatal(err)
		}
		tools = append(tools, declaration)
	}
	declaration, err := canonical.NewMCPToolNamespace(sourceKey, "", source, tools)
	if err != nil {
		t.Fatal(err)
	}
	catalog, _ := declaration.Namespace()
	bindings, attemptTools, _, err := bindingsForCatalog(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != MaxToolsPerSource {
		t.Fatalf("binding count = %d", len(bindings))
	}
	if len(attemptTools) != MaxToolsPerSource {
		t.Fatalf("attempt tool count = %d", len(attemptTools))
	}
	bindingType := reflect.TypeOf(binding{})
	if bindingType.NumField() != 2 ||
		bindingType.Field(0).Type != reflect.TypeOf(canonical.ToolKey{}) ||
		bindingType.Field(1).Type.Kind() != reflect.String {
		t.Fatalf("binding retains aggregate state: %v", bindingType)
	}
}

func TestRunOwnsTransformationWithoutFullRequestSnapshot(t *testing.T) {
	runType := reflect.TypeOf(Run{})
	if _, found := runType.FieldByName("attemptFull"); found {
		t.Fatal("Run retains a stale full-request authority")
	}
	if _, found := reflect.TypeOf((*Run)(nil)).MethodByName("AttemptFull"); found {
		t.Fatal("Run exposes a stale full-request authority")
	}
}

func TestRunRejectsRemoteEffectBudgetBeforeAnotherCall(t *testing.T) {
	request, remoteKey, _ := runtimeTestRequest(t)
	run := runtimeTestRun(t, request, remoteKey)
	run.callCount = MaxRemoteCallsPerRun
	calls, err := run.Calls(runtimeTestResponse(t, runtimeTestCall(t, "call_remote", remoteKey)))
	if err != nil {
		t.Fatal(err)
	}
	err = run.BeginBatch(calls)
	var backend canonical.BackendError
	if !errors.As(err, &backend) {
		t.Fatalf("budget error = %T %v", err, err)
	}
}

func TestRunCanExecuteRequiresAtLeastOneUsableBinding(t *testing.T) {
	if (&Run{}).CanExecute() {
		t.Fatal("empty runtime can execute")
	}
	_, remoteKey, _ := runtimeTestRequest(t)
	run := &Run{
		bindings: map[canonical.ToolKey]binding{
			remoteKey: {remoteName: remoteKey.Name()},
		},
	}
	if !run.CanExecute() {
		t.Fatal("bound runtime cannot execute")
	}
}

func TestAccessRejectsContradictoryBearerWithoutExposingValues(t *testing.T) {
	sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindNamespace, "docs")
	const bearer = "incident-secret-bearer"
	access, err := (Access{}).WithBearer(sourceKey, bearer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := access.WithBearer(sourceKey, bearer); err != nil {
		t.Fatalf("idempotent bearer = %v", err)
	}
	if _, err := access.WithBearer(sourceKey, "second"); err == nil {
		t.Fatal("contradictory bearer was admitted")
	}
	for _, formatted := range []string{
		fmt.Sprintf("%v", access),
		fmt.Sprintf("%#v", access),
		fmt.Sprintf("%v", &Run{}),
		fmt.Sprintf("%#v", &Run{}),
	} {
		if strings.Contains(formatted, bearer) ||
			(formatted != "<mcp-access>" && formatted != "<mcp-run>") {
			t.Fatalf("secret-bearing value formatted as %q", formatted)
		}
	}
	var output bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&output, nil))
	logger.Info("access", "value", access)
	if strings.Contains(output.String(), bearer) ||
		!strings.Contains(output.String(), "<mcp-access>") {
		t.Fatalf("structured access log = %q", output.String())
	}
}

func TestOpenSkipsResolutionWhenToolPolicyDisablesMCP(t *testing.T) {
	request, _, _ := runtimeTestRequest(t)
	request = canonical.NewCanonicalRequest(canonical.RequestParams{
		Items: request.Items(),
		ToolPolicy: canonical.Specify(
			canonical.NewToolPolicy(canonical.ToolPolicyNone, nil),
		),
	})
	resolutions := 0
	prepared, run, _, err := openWith(
		context.Background(), request, Access{},
		func(context.Context, canonical.ToolNamespace, string) (sourceResolution, error) {
			resolutions++
			return sourceResolution{}, errors.New("resolver must not run")
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolutions != 0 {
		t.Fatalf("MCP resolutions = %d", resolutions)
	}
	assertAttemptHasNoMCP(t, prepared, run)
}

func TestOpenSkipsResolutionForExplicitlyEmptyAllowedTools(t *testing.T) {
	request := runtimeTestRequestWithAllowedTools(t, canonical.Specify([]string{}))
	resolutions := 0
	prepared, run, _, err := openWith(
		context.Background(), request, Access{},
		func(context.Context, canonical.ToolNamespace, string) (sourceResolution, error) {
			resolutions++
			return sourceResolution{}, errors.New("resolver must not run")
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if resolutions != 0 {
		t.Fatalf("MCP resolutions = %d", resolutions)
	}
	assertAttemptHasNoMCP(t, prepared, run)
}

func TestOpenDoesNotApplySourceLimitToDisabledMCP(t *testing.T) {
	tests := []struct {
		name    string
		allowed canonical.Specified[[]string]
		policy  canonical.ToolPolicy
	}{
		{
			name:    "tool policy none",
			allowed: canonical.Unspecified[[]string](),
			policy:  canonical.NewToolPolicy(canonical.ToolPolicyNone, nil),
		},
		{
			name:    "explicitly empty allowed tools",
			allowed: canonical.Specify([]string{}),
			policy:  canonical.NewToolPolicy(canonical.ToolPolicyAuto, nil),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := runtimeTestManySources(
				t, MaxSourcesPerRequest+1, test.allowed, test.policy,
			)
			resolutions := 0
			prepared, run, _, err := openWith(
				context.Background(), request, Access{},
				func(context.Context, canonical.ToolNamespace, string) (sourceResolution, error) {
					resolutions++
					return sourceResolution{}, errors.New("resolver must not run")
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			if resolutions != 0 {
				t.Fatalf("MCP resolutions = %d", resolutions)
			}
			assertAttemptHasNoMCP(t, prepared, run)
		})
	}
}

func TestOpenResolvesRequiredSourcesBeforeOptionalSources(t *testing.T) {
	request, requiredKey, sourceOrder := runtimeTestRequiredSourceRequest(t)
	var resolved []canonical.ToolKey
	_, run, _, err := openWith(
		context.Background(), request, Access{},
		func(_ context.Context, source canonical.ToolNamespace, _ string) (sourceResolution, error) {
			resolved = append(resolved, source.Key())
			return sourceResolution{session: &session{}, catalog: source}, nil
		},
	)
	if run != nil {
		defer run.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 2 || resolved[0] != requiredKey ||
		resolved[1] != sourceOrder[0] {
		t.Fatalf("resolution order = %#v", resolved)
	}
}

func TestExplicitEmptyAllowedToolsWithRequiredPolicyIsBadRequest(t *testing.T) {
	request := runtimeTestRequestWithAllowedTools(t, canonical.Specify([]string{}))
	request = canonical.NewCanonicalRequest(canonical.RequestParams{
		Items: request.Items(),
		ToolPolicy: canonical.Specify(
			canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil),
		),
	})
	_, run, _, err := openWith(
		context.Background(), request, Access{},
		func(context.Context, canonical.ToolNamespace, string) (sourceResolution, error) {
			return sourceResolution{}, errors.New("resolver must not run")
		},
	)
	if run != nil {
		defer run.Close()
	}
	var canonicalError canonical.Error
	if !errors.As(err, &canonicalError) ||
		canonicalError.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("required empty selection error = %T %v", err, err)
	}
}

func TestOpenRejectsAggregateCatalogBytesPerRun(t *testing.T) {
	request := runtimeTestManySources(
		t, 5, canonical.Unspecified[[]string](),
		canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil),
	)
	prepared, run, decisions, err := openWith(
		context.Background(), request, Access{},
		func(_ context.Context, source canonical.ToolNamespace, _ string) (sourceResolution, error) {
			catalog := runtimeTestCatalogWithDescription(
				t, source, strings.Repeat("x", MaxCatalogBytesPerSource/2-2048),
			)
			return sourceResolution{session: &session{}, catalog: catalog}, nil
		},
	)
	if run != nil {
		defer run.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	dropped := 0
	for _, decision := range decisions {
		if decision.Outcome == compat.Drop {
			dropped++
		}
	}
	if dropped == 0 {
		t.Fatalf("aggregate catalog decisions = %#v", decisions)
	}
	attemptRequest, err := run.AttemptRequest(prepared)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := canonical.EffectiveTools(attemptRequest)
	if err != nil {
		t.Fatal(err)
	}
	if len(environment.Declarations()) >= 5 {
		t.Fatalf("aggregate catalog retained every source: %d", len(environment.Declarations()))
	}
}

func TestDependencyErrorBoundsRemoteEvidence(t *testing.T) {
	request, _, _ := runtimeTestRequest(t)
	environment, _ := canonical.EffectiveTools(request)
	source := mcpSources(environment)[0]
	err := dependencyError(
		source, errors.New(strings.Repeat("remote", MaxDependencyEvidenceBytes)),
	)
	var backend canonical.BackendError
	if !errors.As(err, &backend) {
		t.Fatalf("dependency error = %T %v", err, err)
	}
	if len(backend.Message) > MaxDependencyEvidenceBytes {
		t.Fatalf("dependency evidence bytes = %d", len(backend.Message))
	}
}

func TestRunDerivesOrdinaryAttemptFunctionsFromFrozenCatalog(t *testing.T) {
	full, remoteKey, _ := runtimeTestRequest(t)
	environment, err := canonical.EffectiveTools(full)
	if err != nil {
		t.Fatal(err)
	}
	sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindNamespace, "docs")
	run := &Run{
		sessions: map[canonical.ToolKey]*session{sourceKey: {}},
		attemptTools: map[canonical.ToolKey][]canonical.ToolDeclaration{
			sourceKey: {mustRuntimeTool(t, environment, remoteKey)},
		},
		bindings: map[canonical.ToolKey]binding{
			remoteKey: {
				source: sourceKey, remoteName: remoteKey.Name(),
			},
		},
	}
	source, _ := canonical.NewMCPSource("https://mcp.example.test/rpc", canonical.Unspecified[[]string]())
	unresolved, _ := canonical.NewMCPToolNamespace(sourceKey, "", source, nil)
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{unresolved})
	item, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	delta := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{item}})

	attempt, err := run.AttemptRequest(delta)
	if err != nil {
		t.Fatal(err)
	}
	attemptEnvironment, err := canonical.EffectiveTools(attempt)
	if err != nil {
		t.Fatal(err)
	}
	declarations := attemptEnvironment.Declarations()
	if len(declarations) != 1 || declarations[0].Key() != remoteKey {
		t.Fatalf("attempt declarations = %#v", declarations)
	}
	if _, ok := declarations[0].Function(); !ok {
		t.Fatalf("attempt declaration is not an ordinary function: %#v", declarations[0])
	}

	canonicalEnvironment, _ := canonical.EffectiveTools(full)
	canonicalSource, _ := canonicalEnvironment.Lookup(sourceKey)
	namespace, _ := canonicalSource.Namespace()
	if _, ok := namespace.MCPSource(); !ok {
		t.Fatal("attempt derivation mutated canonical MCP history")
	}
}

func TestRunDropsUnavailableMCPSourceOnlyFromAttempt(t *testing.T) {
	request, _, localKey := runtimeTestRequest(t)
	run := &Run{sessions: map[canonical.ToolKey]*session{}}

	attempt, err := run.AttemptRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	attemptEnvironment, _ := canonical.EffectiveTools(attempt)
	declarations := attemptEnvironment.Declarations()
	if len(declarations) != 1 || declarations[0].Key() != localKey {
		t.Fatalf("unavailable-source attempt = %#v", declarations)
	}
	originalEnvironment, _ := canonical.EffectiveTools(request)
	if len(originalEnvironment.Declarations()) != 2 {
		t.Fatal("attempt derivation mutated canonical declarations")
	}
}

func TestOpenOptionalFailurePreservesCanonicalHistoryAndDropsOnlyAttempt(t *testing.T) {
	request, remoteKey, localKey := runtimeTestRequest(t)
	prepared, run, decisions, err := openWith(
		context.Background(), request, Access{},
		func(context.Context, canonical.ToolNamespace, string) (sourceResolution, error) {
			return sourceResolution{}, canonical.NewBackendError("mcp:docs", http.StatusBadGateway, "unavailable", "")
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if run == nil || run.CanExecute() {
		t.Fatalf("optional-failure runtime = %#v", run)
	}
	if len(decisions) != 1 || decisions[0].Outcome != compat.Drop {
		t.Fatalf("optional-failure decisions = %#v", decisions)
	}
	fullEnvironment, _ := canonical.EffectiveTools(prepared)
	if _, ok := fullEnvironment.Lookup(remoteKey); !ok {
		t.Fatal("optional source failure stripped frozen canonical history")
	}
	attemptRequest, _ := run.AttemptRequest(prepared)
	attemptEnvironment, _ := canonical.EffectiveTools(attemptRequest)
	declarations := attemptEnvironment.Declarations()
	if len(declarations) != 1 || declarations[0].Key() != localKey {
		t.Fatalf("optional-failure attempt = %#v", declarations)
	}
}

func TestOpenResolvesMCPSourceContributedByDiscoveryResult(t *testing.T) {
	sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindNamespace, "docs")
	remoteKey, _ := canonical.NewToolKey("mcp/docs", canonical.ToolKindFunction, "search")
	source, _ := canonical.NewMCPSource("https://mcp.example.test/rpc", canonical.Unspecified[[]string]())
	unresolved, _ := canonical.NewMCPToolNamespace(sourceKey, "Docs", source, nil)
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{unresolved})
	callID, _ := canonical.NewToolCallID("discovery_1")
	input, _ := canonical.ParseJSONObject([]byte(`{}`))
	call, _ := canonical.NewToolDiscoveryCallItem(callID, canonical.NewJSONObjectToolInput(input), canonical.DiscoveryExecutorProvider)
	result, _ := canonical.NewToolDiscoveryResultItem(callID, set, canonical.DiscoveryExecutorProvider)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{call, result}})
	catalog := runtimeTestCatalog(t, sourceKey, remoteKey, "Docs", "Search")

	prepared, run, _, err := openWith(
		context.Background(), request, Access{},
		func(context.Context, canonical.ToolNamespace, string) (sourceResolution, error) {
			return sourceResolution{session: &session{}, catalog: catalog}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	preparedResult, ok := prepared.Items()[1].ToolDiscoveryResult()
	if !ok || preparedResult.CallID() != callID ||
		preparedResult.Executor() != canonical.DiscoveryExecutorProvider {
		t.Fatalf("prepared discovery carrier = %#v", prepared.Items()[1])
	}
	environment, _ := canonical.EffectiveTools(prepared)
	if _, ok := environment.Lookup(remoteKey); !ok || !run.CanExecute() {
		t.Fatalf("resolved discovery environment = %#v run=%#v", environment.Declarations(), run)
	}
}

func TestOpenRequiredUnavailableSourceFails(t *testing.T) {
	request, remoteKey, _ := runtimeTestRequest(t)
	request = canonical.NewCanonicalRequest(canonical.RequestParams{
		Items:      request.Items(),
		ToolPolicy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicySpecific, &remoteKey)),
	})
	_, run, decisions, err := openWith(
		context.Background(), request, Access{},
		func(context.Context, canonical.ToolNamespace, string) (sourceResolution, error) {
			return sourceResolution{}, canonical.NewBackendError("mcp:docs", http.StatusBadGateway, "unavailable", "")
		},
	)
	if err == nil || run != nil || len(decisions) != 0 {
		t.Fatalf("required unavailable source = run=%#v decisions=%#v err=%v", run, decisions, err)
	}
}

func TestOpenPendingCallToUnavailableSourceFails(t *testing.T) {
	request, remoteKey, _ := runtimeTestRequest(t)
	call := runtimeTestCall(t, "call_pending", remoteKey)
	request = request.WithItems(append(request.Items(), call))
	_, run, decisions, err := openWith(
		context.Background(), request, Access{},
		func(context.Context, canonical.ToolNamespace, string) (sourceResolution, error) {
			return sourceResolution{}, canonical.NewBackendError("mcp:docs", http.StatusBadGateway, "unavailable", "")
		},
	)
	if err == nil || run != nil || len(decisions) != 0 {
		t.Fatalf("pending unavailable source = run=%#v decisions=%#v err=%v", run, decisions, err)
	}
}

func TestOpenRequiredPolicyFailsWhenUnavailableSourcesLeaveNoTools(t *testing.T) {
	request, _, _ := runtimeTestRequest(t)
	environment, _ := canonical.EffectiveTools(request)
	var remote canonical.ToolDeclaration
	for _, declaration := range environment.Declarations() {
		if namespace, ok := declaration.Namespace(); ok {
			if _, ok := namespace.MCPSource(); ok {
				remote = declaration
			}
		}
	}
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{remote})
	item, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	request = canonical.NewCanonicalRequest(canonical.RequestParams{
		Items:      []canonical.CanonicalItem{item},
		ToolPolicy: canonical.Specify(canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil)),
	})
	_, run, decisions, err := openWith(
		context.Background(), request, Access{},
		func(context.Context, canonical.ToolNamespace, string) (sourceResolution, error) {
			return sourceResolution{}, canonical.NewBackendError("mcp:docs", http.StatusBadGateway, "unavailable", "")
		},
	)
	if err == nil || run != nil || len(decisions) != 1 || decisions[0].Outcome != compat.Drop {
		t.Fatalf("required empty attempt = run=%#v decisions=%#v err=%v", run, decisions, err)
	}
}

func TestOpenWithoutMCPReturnsNoRuntime(t *testing.T) {
	key, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "local")
	schemaObject, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	function, _ := canonical.NewFunctionTool(
		key, "", canonical.NewToolSchemaObject(schemaObject), canonical.Unspecified[bool](),
	)
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{function})
	item, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{item}})
	prepared, run, decisions, err := Open(context.Background(), request, Access{})
	if err != nil || run != nil || len(decisions) != 0 {
		t.Fatalf("non-MCP open = run=%#v decisions=%#v err=%v", run, decisions, err)
	}
	environment, _ := canonical.EffectiveTools(prepared)
	if _, ok := environment.Lookup(key); !ok {
		t.Fatal("non-MCP open changed canonical tools")
	}
}

func TestOpenComposesNamespaceDescriptionIntoAttemptFunctions(t *testing.T) {
	request, remoteKey, _ := runtimeTestRequest(t)
	sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindNamespace, "docs")
	catalog := runtimeTestCatalog(t, sourceKey, remoteKey, "Docs server", "Search")
	prepared, run, decisions, err := openWith(
		context.Background(), request, Access{},
		func(context.Context, canonical.ToolNamespace, string) (sourceResolution, error) {
			return sourceResolution{session: &session{}, catalog: catalog}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	attemptRequest, _ := run.AttemptRequest(prepared)
	attempt, _ := canonical.EffectiveTools(attemptRequest)
	declaration, ok := attempt.Lookup(remoteKey)
	if !ok {
		t.Fatal("attempt function is absent")
	}
	function, _ := declaration.Function()
	if function.Description() != "Docs server\n\nSearch" {
		t.Fatalf("attempt description = %q", function.Description())
	}
	foundApprox := false
	for _, decision := range decisions {
		foundApprox = foundApprox || decision.Outcome == compat.Approx &&
			strings.Contains(string(decision.Subject), sourceKey.String())
	}
	if !foundApprox {
		t.Fatalf("namespace-description decisions = %#v", decisions)
	}
}

func runtimeTestRequest(t *testing.T) (canonical.CanonicalRequest, canonical.ToolKey, canonical.ToolKey) {
	t.Helper()
	sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindNamespace, "docs")
	remoteKey, _ := canonical.NewToolKey("mcp/docs", canonical.ToolKindFunction, "search")
	localKey, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "local")
	schemaObject, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	schema := canonical.NewToolSchemaObject(schemaObject)
	remoteFunction, _ := canonical.NewFunctionTool(remoteKey, "", schema, canonical.Unspecified[bool]())
	localFunction, _ := canonical.NewFunctionTool(localKey, "", schema, canonical.Unspecified[bool]())
	source, _ := canonical.NewMCPSource("https://mcp.example.test/rpc", canonical.Unspecified[[]string]())
	namespace, _ := canonical.NewMCPToolNamespace(sourceKey, "", source, []canonical.ToolDeclaration{remoteFunction})
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{namespace, localFunction})
	item, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	return canonical.NewCanonicalRequest(canonical.RequestParams{Items: []canonical.CanonicalItem{item}}), remoteKey, localKey
}

func runtimeTestRun(t *testing.T, request canonical.CanonicalRequest, remoteKey canonical.ToolKey) *Run {
	t.Helper()
	sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindNamespace, "docs")
	return &Run{
		bindings: map[canonical.ToolKey]binding{
			remoteKey: {
				source: sourceKey, remoteName: remoteKey.Name(),
			},
		},
	}
}

func runtimeTestRequestWithAllowedTools(
	t *testing.T,
	allowed canonical.Specified[[]string],
) canonical.CanonicalRequest {
	t.Helper()
	sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindNamespace, "docs")
	source, err := canonical.NewMCPSource("https://mcp.example.test/rpc", allowed)
	if err != nil {
		t.Fatal(err)
	}
	namespace, err := canonical.NewMCPToolNamespace(sourceKey, "", source, nil)
	if err != nil {
		t.Fatal(err)
	}
	set, _ := canonical.NewToolSet([]canonical.ToolDeclaration{namespace})
	item, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Items: []canonical.CanonicalItem{item},
	})
}

func runtimeTestManySources(
	t *testing.T,
	count int,
	allowed canonical.Specified[[]string],
	policy canonical.ToolPolicy,
) canonical.CanonicalRequest {
	t.Helper()
	declarations := make([]canonical.ToolDeclaration, 0, count)
	for index := 0; index < count; index++ {
		sourceKey, _ := canonical.NewToolKey(
			"mcp", canonical.ToolKindNamespace, fmt.Sprintf("source_%02d", index),
		)
		source, err := canonical.NewMCPSource(
			fmt.Sprintf("https://mcp-%02d.example.test/rpc", index), allowed,
		)
		if err != nil {
			t.Fatal(err)
		}
		namespace, err := canonical.NewMCPToolNamespace(sourceKey, "", source, nil)
		if err != nil {
			t.Fatal(err)
		}
		declarations = append(declarations, namespace)
	}
	set, err := canonical.NewToolSet(declarations)
	if err != nil {
		t.Fatal(err)
	}
	item, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Items:      []canonical.CanonicalItem{item},
		ToolPolicy: canonical.Specify(policy),
	})
}

func runtimeTestRequiredSourceRequest(
	t *testing.T,
) (canonical.CanonicalRequest, canonical.ToolKey, []canonical.ToolKey) {
	t.Helper()
	schemaObject, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	schema := canonical.NewToolSchemaObject(schemaObject)
	var declarations []canonical.ToolDeclaration
	var sourceKeys []canonical.ToolKey
	var requiredTool canonical.ToolKey
	for _, name := range []string{"optional", "required"} {
		sourceKey, _ := canonical.NewToolKey("mcp", canonical.ToolKindNamespace, name)
		toolKey, _ := canonical.NewToolKey(
			"mcp/"+name, canonical.ToolKindFunction, "call",
		)
		tool, _ := canonical.NewFunctionTool(
			toolKey, "", schema, canonical.Unspecified[bool](),
		)
		source, _ := canonical.NewMCPSource(
			"https://"+name+".example.test/rpc",
			canonical.Unspecified[[]string](),
		)
		namespace, _ := canonical.NewMCPToolNamespace(
			sourceKey, "", source, []canonical.ToolDeclaration{tool},
		)
		declarations = append(declarations, namespace)
		sourceKeys = append(sourceKeys, sourceKey)
		if name == "required" {
			requiredTool = toolKey
		}
	}
	set, _ := canonical.NewToolSet(declarations)
	item, _ := canonical.NewToolDeclarationsItem(set, canonical.ContextScopeRequest)
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Items: []canonical.CanonicalItem{item},
		ToolPolicy: canonical.Specify(
			canonical.NewToolPolicy(canonical.ToolPolicySpecific, &requiredTool),
		),
	}), sourceKeys[1], sourceKeys
}

func runtimeTestCatalogWithDescription(
	t *testing.T,
	source canonical.ToolNamespace,
	description string,
) canonical.ToolNamespace {
	t.Helper()
	schemaObject, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	key, _ := canonical.NewToolKey(
		source.Key().Namespace()+"/"+source.Key().Name(),
		canonical.ToolKindFunction,
		"tool",
	)
	tool, err := canonical.NewFunctionTool(
		key, description, canonical.NewToolSchemaObject(schemaObject),
		canonical.Unspecified[bool](),
	)
	if err != nil {
		t.Fatal(err)
	}
	remote, _ := source.MCPSource()
	declaration, err := canonical.NewMCPToolNamespace(
		source.Key(), source.Description(), remote,
		[]canonical.ToolDeclaration{tool},
	)
	if err != nil {
		t.Fatal(err)
	}
	catalog, _ := declaration.Namespace()
	return catalog
}

func runtimeTestCatalog(
	t *testing.T,
	sourceKey canonical.ToolKey,
	remoteKey canonical.ToolKey,
	sourceDescription string,
	toolDescription string,
) canonical.ToolNamespace {
	t.Helper()
	schemaObject, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	function, err := canonical.NewFunctionTool(
		remoteKey, toolDescription, canonical.NewToolSchemaObject(schemaObject),
		canonical.Unspecified[bool](),
	)
	if err != nil {
		t.Fatal(err)
	}
	source, _ := canonical.NewMCPSource("https://mcp.example.test/rpc", canonical.Unspecified[[]string]())
	declaration, err := canonical.NewMCPToolNamespace(
		sourceKey, sourceDescription, source, []canonical.ToolDeclaration{function},
	)
	if err != nil {
		t.Fatal(err)
	}
	namespace, _ := declaration.Namespace()
	return namespace
}

func mustRuntimeTool(t *testing.T, environment canonical.ToolEnvironment, key canonical.ToolKey) canonical.ToolDeclaration {
	t.Helper()
	declaration, ok := environment.Lookup(key)
	if !ok {
		t.Fatalf("tool %s is absent", key.String())
	}
	return declaration
}

func runtimeTestCall(t *testing.T, id string, key canonical.ToolKey) canonical.CanonicalItem {
	t.Helper()
	callID, _ := canonical.NewToolCallID(id)
	input, _ := canonical.ParseJSONObject([]byte(`{}`))
	call, err := canonical.NewToolCallItem(callID, key, canonical.NewJSONObjectToolInput(input))
	if err != nil {
		t.Fatal(err)
	}
	return call
}

func runtimeTestTextCall(t *testing.T, id string, key canonical.ToolKey) canonical.CanonicalItem {
	t.Helper()
	callID, _ := canonical.NewToolCallID(id)
	call, err := canonical.NewToolCallItem(callID, key, canonical.NewTextToolInput(""))
	if err != nil {
		t.Fatal(err)
	}
	return call
}

func runtimeTestWebSearchLifecycle(t *testing.T, id string) (canonical.CanonicalItem, canonical.CanonicalItem) {
	t.Helper()
	callID, _ := canonical.NewToolCallID(id)
	input, err := canonical.NewWebSearchToolInput(
		canonical.WebSearchCall{Action: canonical.WebSearchActionSearch},
	)
	if err != nil {
		t.Fatal(err)
	}
	call, err := canonical.NewToolCallItem(callID, canonical.WebSearchToolKey(), input)
	if err != nil {
		t.Fatal(err)
	}
	searchResult, err := canonical.NewWebSearchResult(nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := canonical.NewWebSearchResultItem(callID, searchResult)
	if err != nil {
		t.Fatal(err)
	}
	return call, result
}

func runtimeTestDiscoveryLifecycle(
	t *testing.T,
	id string,
	executor canonical.DiscoveryExecutor,
) (canonical.CanonicalItem, canonical.CanonicalItem) {
	t.Helper()
	callID, _ := canonical.NewToolCallID(id)
	input, _ := canonical.ParseJSONObject([]byte(`{}`))
	call, err := canonical.NewToolDiscoveryCallItem(
		callID, canonical.NewJSONObjectToolInput(input), executor,
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := canonical.NewToolDiscoveryResultItem(
		callID, canonical.ToolSet{}, executor,
	)
	if err != nil {
		t.Fatal(err)
	}
	return call, result
}

func assertAttemptHasNoMCP(
	t *testing.T,
	prepared canonical.CanonicalRequest,
	run *Run,
) {
	t.Helper()
	if run == nil {
		t.Fatal("disabled MCP request has no attempt-view runtime")
	}
	preparedEnvironment, err := canonical.EffectiveTools(prepared)
	if err != nil || len(mcpSources(preparedEnvironment)) == 0 {
		t.Fatalf("canonical history lost MCP source: %#v, err = %v", preparedEnvironment.Declarations(), err)
	}
	attemptRequest, err := run.AttemptRequest(prepared)
	if err != nil {
		t.Fatal(err)
	}
	attemptEnvironment, err := canonical.EffectiveTools(attemptRequest)
	if err != nil {
		t.Fatal(err)
	}
	if len(mcpSources(attemptEnvironment)) != 0 || run.CanExecute() {
		t.Fatalf("disabled attempt retained MCP: %#v", attemptEnvironment.Declarations())
	}
}

func runtimeTestResponse(t *testing.T, items ...canonical.CanonicalItem) canonical.CanonicalResponse {
	t.Helper()
	response, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: "resp_mcp"}, "model", items, canonical.Completed("tool_calls"),
		canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return response
}
