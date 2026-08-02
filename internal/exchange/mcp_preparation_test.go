package exchange

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
	"github.com/swobuforge/swobu/internal/session"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestMCPPreparationRetainsIngressAccessForNativeAttempts(t *testing.T) {
	source, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "docs")
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
	if !reflect.DeepEqual(outcome.nextState.input.mcpAccess, access) {
		t.Fatal("exchange discarded ingress MCP access before native target projection")
	}
}

func TestMCPAccessIsOpaqueInsideExchangeContainers(t *testing.T) {
	source, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "docs")
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
	environment, err := canonical.EffectiveTools(call.decodeContext)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := environment.Lookup(key); !ok {
		t.Fatal("current full discovery result is absent from provider decode context")
	}
}

func TestProviderPreparationRebuildsSameToolNamesForFullHistoryNativeDeltaAndImageResume(t *testing.T) {
	currentKey, _ := canonical.NewToolKey("workspace", canonical.ToolKindFunction, "search")
	historicalKey, _ := canonical.NewToolKey("history/legacy", canonical.ToolKindCustom, "apply")
	current := canonicaltest.MustFunctionTool(
		currentKey, "", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool](),
	)
	declarations := canonicaltest.ToolDeclarations(t, current)
	callID, _ := canonical.NewToolCallID("call_historical")
	historicalCall, _ := canonical.NewToolCallItem(callID, historicalKey, canonical.NewTextToolInput("old"))
	imageRequest, imageBytes := testURLImageRequest(t, "https://example.test/current.png")
	imageItem := imageRequest.Items()[0]
	full := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{declarations, historicalCall, imageItem},
	})

	target := requestpathTarget(t, "stable-tool-names")
	path, err := resolveProviderPath(target)
	if err != nil {
		t.Fatal(err)
	}
	delta := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{imageItem},
		PreviousResponse: &canonical.ResponseRef{
			SwobuID: "resp_previous",
			Responses: &canonical.ResponsesContinuation{
				ProviderResponseID: canonical.NewResponsesResponseID("provider_previous"),
				TargetID:           path.target.TargetID, TargetVersion: path.target.TargetVersion,
			},
		},
	})
	prepared := session.ResolvedRequest{Full: full, Delta: delta}
	state := reducerTestState(t)
	state.input.request = delta
	state.prepared = &prepared
	state.route = routePlan{targets: []routing.Target{target}}
	runner := withRuntime(bufferedProviderTransport(nil))
	runner.ImageFetcher = &fixedImageFetcher{fetched: provider.FetchedImageResult{
		DeclaredMediaType: canonical.ImageMediaPNG, Bytes: imageBytes,
	}}

	aliases := make(map[providerRequestChoice]map[canonical.ToolKey]string)
	for _, choice := range []providerRequestChoice{providerRequestFullHistory, providerRequestPreferred} {
		selection := providerCallSelection{candidateIndex: 0, requestChoice: choice}
		_, _, _, deferred, err := prepareProviderCall(state, selection, runner, nil)
		if err != nil {
			t.Fatal(err)
		}
		materialize, ok := deferred.(materializeAttemptImagesCommand)
		if !ok {
			t.Fatalf("choice %d preparation = %T, want image materialization", choice, deferred)
		}
		event, ok := executeCommand(context.Background(), materialize).(attemptImagesMaterialized)
		if !ok || event.err != nil {
			t.Fatalf("choice %d materialization = %#v", choice, event)
		}
		call, _, _, next, err := prepareProviderCall(state, selection, runner, &event)
		if err != nil {
			t.Fatal(err)
		}
		if next != nil {
			t.Fatalf("choice %d preparation deferred twice: %T", choice, next)
		}
		aliases[choice] = make(map[canonical.ToolKey]string)
		for _, key := range []canonical.ToolKey{currentKey, historicalKey} {
			name, err := call.request.ToolNames.WireName(key)
			if err != nil {
				t.Fatalf("choice %d key %q: %v", choice, key, err)
			}
			aliases[choice][key] = name
		}
	}
	for _, key := range []canonical.ToolKey{currentKey, historicalKey} {
		if aliases[providerRequestFullHistory][key] != aliases[providerRequestPreferred][key] {
			t.Fatalf("tool %q changed between full history and native delta: %#v", key, aliases)
		}
	}
}
