package exchange

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestMCPPreparationDiscardsIngressAccessAfterOpen(t *testing.T) {
	source, _ := canonical.NewToolKey("mcp", canonical.ToolKindNamespace, "docs")
	access, err := (mcp.Access{}).WithBearer(source, "incident-secret-bearer")
	if err != nil {
		t.Fatal(err)
	}
	state := exchangeState{
		input: exchangeInput{mcpAccess: access},
		phase: preparingMCPPhase{},
	}
	outcome, err := reducePreparingMCP(
		state,
		mcpPrepared{err: errors.New("stop after MCP open")},
		runtimeBundle{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(outcome.nextState.input.mcpAccess, mcp.Access{}) {
		t.Fatal("exchange retained ingress MCP access after Open completed")
	}
}

func TestMCPAccessIsOpaqueInsideExchangeContainers(t *testing.T) {
	source, _ := canonical.NewToolKey("mcp", canonical.ToolKindNamespace, "docs")
	const bearer = "nested-exchange-secret"
	access, err := (mcp.Access{}).WithBearer(source, bearer)
	if err != nil {
		t.Fatal(err)
	}
	values := []any{
		prepareMCPCommand{access: access},
		exchangeInput{mcpAccess: access},
	}
	for _, value := range values {
		for _, formatted := range []string{
			fmt.Sprintf("%v", value),
			fmt.Sprintf("%+v", value),
			fmt.Sprintf("%#v", value),
		} {
			if strings.Contains(formatted, bearer) {
				t.Fatalf("%T exposed bearer as %q", value, formatted)
			}
		}
	}
}

func TestProviderPreparationProjectsCurrentFullAfterMCPRound(t *testing.T) {
	request := testCanonicalRequest("m")
	key, _ := canonical.NewRequestToolKey(
		canonical.ToolKindFunction, "newly_discovered",
	)
	schemaObject, _ := canonical.ParseJSONObject([]byte(`{"type":"object"}`))
	declaration, err := canonical.NewFunctionTool(
		key, "", canonical.NewToolSchemaObject(schemaObject),
		canonical.Unspecified[bool](),
	)
	if err != nil {
		t.Fatal(err)
	}
	tools, _ := canonical.NewToolSet([]canonical.ToolDeclaration{declaration})
	callID, _ := canonical.NewToolCallID("discovery_current_full")
	input, _ := canonical.ParseJSONObject([]byte(`{}`))
	discoveryCall, err := canonical.NewToolDiscoveryCallItem(
		callID, canonical.NewJSONObjectToolInput(input), canonical.DiscoveryExecutorProvider,
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := canonical.NewToolDiscoveryResultItem(
		callID, tools, canonical.DiscoveryExecutorProvider,
	)
	if err != nil {
		t.Fatal(err)
	}
	request = request.WithItems(append(request.Items(), discoveryCall, result))
	prepared := mustBeginSession(t, request)
	state := reducerTestState(t)
	state.input.request = request
	state.prepared = &prepared
	state.route = routePlan{targets: []routing.Target{
		requestpathTarget(t, "current-full"),
	}}
	state.mcp = &mcp.Run{}

	call, _, _, preparation, err := prepareProviderCall(
		state,
		providerCallSelection{
			candidateIndex: 0, requestChoice: providerRequestFullHistory,
		},
		withRuntime(bufferedProviderTransport(nil)),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if preparation != nil {
		t.Fatalf("provider preparation unexpectedly deferred: %T", preparation)
	}
	environment, err := canonical.EffectiveTools(call.projectedDecodeContext)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := environment.Lookup(key); !ok {
		t.Fatal("current full discovery result is absent from provider decode context")
	}
}
