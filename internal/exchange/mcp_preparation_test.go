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
		context.Background(), state,
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

	call, _, _, _, err := prepareProviderCall(
		context.Background(), state,
		providerCallSelection{
			candidateIndex: 0, requestChoice: providerRequestFullHistory,
		},
		withRuntime(bufferedProviderTransport(nil)),
	)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := canonical.EffectiveTools(call.decodeContext)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := environment.Lookup(key); !ok {
		t.Fatal("current full discovery result is absent from provider decode context")
	}
}

func TestProviderPreparationRebuildsSameToolNamesForFullHistoryPreferredAttemptAndImageResume(t *testing.T) {
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
	target := requestpathTarget(t, "stable-tool-names")
	currentRequest := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{declarations, imageItem},
	})
	prepared := mustResumeSession(t,
		canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{declarations}}),
		[]canonical.CanonicalItem{historicalCall},
		currentRequest,
		target,
	)
	state := reducerTestState(t)
	state.input.request = currentRequest
	state.prepared = &prepared
	state.route = routePlan{targets: []routing.Target{target}}
	runner := withRuntime(bufferedProviderTransport(nil))
	runner.ImageFetcher = &fixedImageFetcher{fetched: provider.FetchedImageResult{
		DeclaredMediaType: canonical.ImageMediaPNG, Bytes: imageBytes,
	}}

	aliases := make(map[providerRequestChoice]map[canonical.ToolKey]string)
	for _, choice := range []providerRequestChoice{providerRequestFullHistory, providerRequestPreferred} {
		selection := providerCallSelection{candidateIndex: 0, requestChoice: choice}
		call, _, _, cache, err := prepareProviderCall(context.Background(), state, selection, runner)
		if err != nil {
			t.Fatal(err)
		}
		state.mediaFetchCache = cloneMediaFetchCache(cache)
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
			t.Fatalf("tool %q changed between full history and preferred attempt: %#v", key, aliases)
		}
	}
}
