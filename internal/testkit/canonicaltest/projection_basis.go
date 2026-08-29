package canonicaltest

import (
	"fmt"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// ProjectionWitness is one constructor-valid provider-facing canonical state.
type ProjectionWitness struct {
	Name    string
	Request canonical.CanonicalRequest
}

func ProjectionBasis(t testing.TB, model string) []ProjectionWitness {
	t.Helper()
	functionKey := MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	customKey := MustRequestToolKey(canonical.ToolKindCustom, "shell")
	function := MustFunctionTool(functionKey, "lookup", Schema(t, `{"type":"object","properties":{"q":{"type":"string"}}}`), canonical.Unspecified[bool]())
	custom := MustCustomTool(customKey, "shell", MustToolFormat(`{"type":"text"}`))
	discovery, err := canonical.NewToolDiscoveryTool("discover", Schema(t, `{"type":"object","properties":{"query":{"type":"string"}}}`), canonical.DiscoveryExecutorClient)
	if err != nil {
		t.Fatal(err)
	}
	providerDiscovery, err := canonical.NewToolDiscoveryTool("provider_discover", Schema(t, `{"type":"object","properties":{"query":{"type":"string"}}}`), canonical.DiscoveryExecutorProvider)
	if err != nil {
		t.Fatal(err)
	}
	regexDiscovery, err := canonical.NewToolDiscoveryToolWithQuery("regex discover", Schema(t, `{"type":"object"}`), canonical.DiscoveryExecutorProvider, canonical.ToolDiscoveryQueryRegex)
	if err != nil {
		t.Fatal(err)
	}
	naturalLanguageDiscovery, err := canonical.NewToolDiscoveryToolWithQuery("natural language discover", Schema(t, `{"type":"object"}`), canonical.DiscoveryExecutorProvider, canonical.ToolDiscoveryQueryNaturalLanguage)
	if err != nil {
		t.Fatal(err)
	}
	namespaceKey := MustRequestToolKey(canonical.ToolKindNamespace, "workspace")
	namespacedFunction := MustFunctionTool(MustRequestToolKey(canonical.ToolKindFunction, "workspace_lookup"), "lookup", Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	namespace, err := canonical.NewToolNamespace(namespaceKey, "workspace", []canonical.ToolDeclaration{namespacedFunction})
	if err != nil {
		t.Fatal(err)
	}

	urlImage, err := canonical.NewURLImage("https://example.test/input.png", canonical.Specify(canonical.ImageDetailHigh))
	if err != nil {
		t.Fatal(err)
	}
	inlineImage, err := canonical.NewInlineImage(canonical.ImageMediaPNG, []byte("PNG"), canonical.Unspecified[canonical.ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	userImage, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{
		canonical.NewTextMessagePart("inspect"), canonical.NewImageMessagePart(urlImage), canonical.NewImageMessagePart(inlineImage),
	})
	if err != nil {
		t.Fatal(err)
	}

	functionCallID, _ := canonical.NewToolCallID("call_function")
	functionCall, _ := canonical.NewToolCallItem(functionCallID, functionKey, canonical.NewJSONObjectToolInput(Object(t, `{"q":"value"}`)))
	functionResult, _ := canonical.NewToolResultItem(functionCallID, []canonical.ToolResultPart{
		canonical.NewTextToolResultPart("found"), canonical.NewImageToolResultPart(urlImage),
	}, false)
	textResultID, _ := canonical.NewToolCallID("call_text_result")
	textResultCall, _ := canonical.NewToolCallItem(textResultID, functionKey, canonical.NewJSONObjectToolInput(Object(t, `{"q":"text"}`)))
	textResult, _ := canonical.NewToolResultItem(textResultID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("text")}, false)
	imageResultID, _ := canonical.NewToolCallID("call_image_result")
	imageResultCall, _ := canonical.NewToolCallItem(imageResultID, functionKey, canonical.NewJSONObjectToolInput(Object(t, `{"q":"image"}`)))
	imageResult, _ := canonical.NewToolResultItem(imageResultID, []canonical.ToolResultPart{canonical.NewImageToolResultPart(inlineImage)}, false)
	customCallID, _ := canonical.NewToolCallID("call_custom")
	customCall, _ := canonical.NewToolCallItem(customCallID, customKey, canonical.NewTextToolInput("echo exact"))
	customResult, _ := canonical.NewToolResultItem(customCallID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("ok")}, false)
	discoveryCallID, _ := canonical.NewToolCallID("call_discovery")
	discoveryCall, _ := canonical.NewToolDiscoveryCallItem(discoveryCallID, canonical.NewJSONObjectToolInput(Object(t, `{"query":"files"}`)), canonical.DiscoveryExecutorClient)
	discovered := MustFunctionTool(MustRequestToolKey(canonical.ToolKindFunction, "read_file"), "read", Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())
	discoveryResult, _ := canonical.NewToolDiscoveryResultItem(discoveryCallID, ToolSet(t, discovered), canonical.DiscoveryExecutorClient)
	providerDiscoveryCallID, _ := canonical.NewToolCallID("call_provider_discovery")
	providerDiscoveryCall, _ := canonical.NewToolDiscoveryCallItem(providerDiscoveryCallID, canonical.NewJSONObjectToolInput(Object(t, `{"query":"files"}`)), canonical.DiscoveryExecutorProvider)
	providerDiscoveryResult, _ := canonical.NewToolDiscoveryResultItem(providerDiscoveryCallID, ToolSet(t, discovered), canonical.DiscoveryExecutorProvider)
	parallelCallID, _ := canonical.NewToolCallID("call_parallel")
	parallelCall, _ := canonical.NewToolCallItem(parallelCallID, functionKey, canonical.NewJSONObjectToolInput(Object(t, `{"q":"other"}`)))
	parallelResult, _ := canonical.NewToolResultItem(parallelCallID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("other")}, false)

	searchCallID, _ := canonical.NewToolCallID("call_search")
	searchInput, _ := canonical.NewWebSearchToolInput(canonical.WebSearchCall{Action: canonical.WebSearchActionSearch, Queries: []string{"swobu"}})
	searchCall, _ := canonical.NewToolCallItem(searchCallID, canonical.WebSearchToolKey(), searchInput)
	searchValue, _ := canonical.NewWebSearchResult(nil)
	searchResult, _ := canonical.NewWebSearchResultItem(searchCallID, searchValue)
	multiCallID, _ := canonical.NewToolCallID("call_multi_search")
	multiInput, _ := canonical.NewWebSearchToolInput(canonical.WebSearchCall{Action: canonical.WebSearchActionSearch, Queries: []string{"one", "two"}})
	multiCall, _ := canonical.NewToolCallItem(multiCallID, canonical.WebSearchToolKey(), multiInput)
	multiValue, _ := canonical.NewWebSearchResult(nil)
	multiResult, _ := canonical.NewWebSearchResultItem(multiCallID, multiValue)
	webURL, _ := canonical.NewWebURL("https://example.test/page")
	openPageCallID, _ := canonical.NewToolCallID("call_open_page")
	openPageInput, _ := canonical.NewWebSearchToolInput(canonical.WebSearchCall{Action: canonical.WebSearchActionOpenPage, URL: canonical.Specify(webURL)})
	openPageCall, _ := canonical.NewToolCallItem(openPageCallID, canonical.WebSearchToolKey(), openPageInput)
	findInPageCallID, _ := canonical.NewToolCallID("call_find_in_page")
	findInPageInput, _ := canonical.NewWebSearchToolInput(canonical.WebSearchCall{Action: canonical.WebSearchActionFindInPage, URL: canonical.Specify(webURL), Match: canonical.Specify("needle")})
	findInPageCall, _ := canonical.NewToolCallItem(findInPageCallID, canonical.WebSearchToolKey(), findInPageInput)

	summary, _ := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "summary")
	trace, _ := canonical.NewReasoningPart(canonical.ReasoningPartTrace, "trace")
	reasoningItem, _ := canonical.NewReasoningItem([]canonical.ReasoningPart{summary, trace}, canonical.OpaqueThinking{})
	opaque, err := canonical.NewResponsesOpaqueThinking(canonical.ResponsesReasoningReplay{EncryptedContent: "cipher", ItemID: "rs_1"})
	if err != nil {
		t.Fatal(err)
	}
	opaqueReasoning, err := canonical.NewReasoningItem(nil, opaque)
	if err != nil {
		t.Fatal(err)
	}
	disabled, _ := canonical.NewReasoningControls(canonical.ReasoningControlsParams{Compute: canonical.Specify(canonical.NewDisabledReasoningCompute())})
	budget, _ := canonical.NewBudgetReasoningCompute(1024)
	reasoning, _ := canonical.NewReasoningControls(canonical.ReasoningControlsParams{Compute: canonical.Specify(budget)})

	jsonObject, _ := canonical.NewOutputFormat(canonical.OutputFormatParams{Kind: canonical.OutputFormatJSONObject})
	jsonSchema, _ := canonical.NewOutputFormat(canonical.OutputFormatParams{Kind: canonical.OutputFormatJSONSchema, Name: "answer", Schema: canonical.NewRawJSONObject(`{"type":"object"}`)})
	textFormat, _ := canonical.NewOutputFormat(canonical.OutputFormatParams{Kind: canonical.OutputFormatText})

	base := func(items []canonical.CanonicalItem) canonical.RequestParams {
		return canonical.RequestParams{Model: canonical.Specify(model), Items: items}
	}
	mixedTools := []canonical.ToolDeclaration{function, custom, discovery, namespace, canonical.NewWebSearchDeclaration()}
	witnesses := []ProjectionWitness{
		{Name: "message roles and media", Request: canonical.NewCanonicalRequest(base([]canonical.CanonicalItem{
			MustInstruction(canonical.MessageRoleSystem, "system"), MustInstruction(canonical.MessageRoleDeveloper, "developer"),
			userImage, Message(t, canonical.MessageRoleAssistant, "answer"),
		}))},
		{Name: "mixed callable relations", Request: canonical.NewCanonicalRequest(base([]canonical.CanonicalItem{
			ToolDeclarations(t, mixedTools...), Message(t, canonical.MessageRoleUser, "use tools"), functionCall, functionResult, customCall, customResult, discoveryCall, discoveryResult,
		}))},
		{Name: "provider discovery effect", Request: canonical.NewCanonicalRequest(base([]canonical.CanonicalItem{ToolDeclarations(t, providerDiscovery), Message(t, canonical.MessageRoleUser, "discover"), providerDiscoveryCall, providerDiscoveryResult}))},
		{Name: "regex provider discovery", Request: canonical.NewCanonicalRequest(base([]canonical.CanonicalItem{ToolDeclarations(t, regexDiscovery), Message(t, canonical.MessageRoleUser, "discover")}))},
		{Name: "natural language provider discovery", Request: canonical.NewCanonicalRequest(base([]canonical.CanonicalItem{ToolDeclarations(t, naturalLanguageDiscovery), Message(t, canonical.MessageRoleUser, "discover")}))},
		{Name: "open callable effect", Request: canonical.NewCanonicalRequest(base([]canonical.CanonicalItem{ToolDeclarations(t, function), Message(t, canonical.MessageRoleUser, "call"), functionCall}))},
		{Name: "text tool result", Request: canonical.NewCanonicalRequest(base([]canonical.CanonicalItem{ToolDeclarations(t, function), Message(t, canonical.MessageRoleUser, "call"), textResultCall, textResult}))},
		{Name: "image tool result", Request: canonical.NewCanonicalRequest(base([]canonical.CanonicalItem{ToolDeclarations(t, function), Message(t, canonical.MessageRoleUser, "call"), imageResultCall, imageResult}))},
		{Name: "mixed tool result", Request: canonical.NewCanonicalRequest(base([]canonical.CanonicalItem{ToolDeclarations(t, function), Message(t, canonical.MessageRoleUser, "call"), functionCall, functionResult}))},
		{Name: "parallel callable effects", Request: canonical.NewCanonicalRequest(base([]canonical.CanonicalItem{ToolDeclarations(t, function), Message(t, canonical.MessageRoleUser, "call"), functionCall, parallelCall, functionResult, parallelResult}))},
		{Name: "ordinary settled search", Request: canonical.NewCanonicalRequest(base([]canonical.CanonicalItem{ToolDeclarations(t, canonical.NewWebSearchDeclaration()), Message(t, canonical.MessageRoleUser, "search"), searchCall, searchResult}))},
		{Name: "open search", Request: canonical.NewCanonicalRequest(base([]canonical.CanonicalItem{ToolDeclarations(t, canonical.NewWebSearchDeclaration()), Message(t, canonical.MessageRoleUser, "search"), searchCall}))},
		{Name: "open page search", Request: canonical.NewCanonicalRequest(base([]canonical.CanonicalItem{ToolDeclarations(t, canonical.NewWebSearchDeclaration()), Message(t, canonical.MessageRoleUser, "open"), openPageCall}))},
		{Name: "find in page search", Request: canonical.NewCanonicalRequest(base([]canonical.CanonicalItem{ToolDeclarations(t, canonical.NewWebSearchDeclaration()), Message(t, canonical.MessageRoleUser, "find"), findInPageCall}))},
		{Name: "multi-query search", Request: canonical.NewCanonicalRequest(base([]canonical.CanonicalItem{ToolDeclarations(t, canonical.NewWebSearchDeclaration()), Message(t, canonical.MessageRoleUser, "search"), multiCall, multiResult}))},
		{Name: "portable reasoning", Request: canonical.NewCanonicalRequest(base([]canonical.CanonicalItem{reasoningItem, Message(t, canonical.MessageRoleUser, "continue")}))},
		{Name: "opaque reasoning replay", Request: canonical.NewCanonicalRequest(base([]canonical.CanonicalItem{opaqueReasoning, Message(t, canonical.MessageRoleUser, "continue")}))},
		{Name: "reasoning disabled", Request: canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify(model), Items: []canonical.CanonicalItem{Message(t, canonical.MessageRoleUser, "answer")}, Reasoning: disabled})},
		{Name: "reasoning automatic", Request: canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify(model), Items: []canonical.CanonicalItem{Message(t, canonical.MessageRoleUser, "answer")}, Reasoning: mustReasoningControls(t, canonical.NewAutomaticReasoningCompute())})},
		{Name: "reasoning budget", Request: canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify(model), Items: []canonical.CanonicalItem{Message(t, canonical.MessageRoleUser, "answer")}, Reasoning: reasoning})},
		{Name: "output text", Request: requestWithOutput(t, base([]canonical.CanonicalItem{Message(t, canonical.MessageRoleUser, "answer")}), textFormat)},
		{Name: "output json object", Request: requestWithOutput(t, base([]canonical.CanonicalItem{Message(t, canonical.MessageRoleUser, "answer")}), jsonObject)},
		{Name: "output json schema", Request: requestWithOutput(t, base([]canonical.CanonicalItem{Message(t, canonical.MessageRoleUser, "answer")}), jsonSchema)},
	}
	for _, policy := range []struct {
		name string
		mode canonical.ToolPolicyMode
		key  *canonical.ToolKey
	}{
		{"none", canonical.ToolPolicyNone, nil}, {"auto", canonical.ToolPolicyAuto, nil}, {"required", canonical.ToolPolicyRequired, nil}, {"specific", canonical.ToolPolicySpecific, &functionKey},
	} {
		params := base([]canonical.CanonicalItem{ToolDeclarations(t, function), Message(t, canonical.MessageRoleUser, "answer")})
		params.ToolPolicy = canonical.Specify(canonical.NewToolPolicy(policy.mode, policy.key))
		witnesses = append(witnesses, ProjectionWitness{Name: "tool policy " + policy.name, Request: canonical.NewCanonicalRequest(params)})
	}
	witnesses = append(witnesses,
		ProjectionWitness{Name: "output and batch unspecified", Request: canonical.NewCanonicalRequest(base([]canonical.CanonicalItem{Message(t, canonical.MessageRoleUser, "answer")}))},
		ProjectionWitness{Name: "at most one tool call", Request: canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: canonical.Specify(model), Items: []canonical.CanonicalItem{ToolDeclarations(t, function), Message(t, canonical.MessageRoleUser, "answer")},
			ToolCallBatch: canonical.Specify(canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne)),
		})},
	)
	for _, effort := range []canonical.InferenceEffort{
		canonical.InferenceEffortMinimal,
		canonical.InferenceEffortLow,
		canonical.InferenceEffortMedium,
		canonical.InferenceEffortHigh,
		canonical.InferenceEffortXHigh,
		canonical.InferenceEffortMax,
	} {
		controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{Effort: &effort})
		if err != nil {
			t.Fatal(err)
		}
		witnesses = append(witnesses, ProjectionWitness{
			Name: "inference effort " + string(effort),
			Request: canonical.NewCanonicalRequest(canonical.RequestParams{
				Model: canonical.Specify(model), Items: []canonical.CanonicalItem{Message(t, canonical.MessageRoleUser, "answer")}, Controls: controls,
			}),
		})
	}
	for _, disclosure := range []canonical.ReasoningDisclosure{canonical.ReasoningDisclosureNone, canonical.ReasoningDisclosureSummary} {
		controls, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{Disclosure: canonical.Specify(disclosure)})
		if err != nil {
			t.Fatal(err)
		}
		witnesses = append(witnesses, ProjectionWitness{
			Name: "reasoning disclosure " + string(disclosure),
			Request: canonical.NewCanonicalRequest(canonical.RequestParams{
				Model: canonical.Specify(model), Items: []canonical.CanonicalItem{Message(t, canonical.MessageRoleUser, "answer")}, Reasoning: controls,
			}),
		})
	}
	for _, reasoningContext := range []canonical.ResponsesReasoningContext{
		canonical.ResponsesReasoningContextAuto,
		canonical.ResponsesReasoningContextAllTurns,
		canonical.ResponsesReasoningContextCurrentTurn,
	} {
		controls, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{ResponsesContext: canonical.Specify(reasoningContext)})
		if err != nil {
			t.Fatal(err)
		}
		witnesses = append(witnesses, ProjectionWitness{
			Name: "responses reasoning context " + string(reasoningContext),
			Request: canonical.NewCanonicalRequest(canonical.RequestParams{
				Model: canonical.Specify(model), Items: []canonical.CanonicalItem{Message(t, canonical.MessageRoleUser, "answer")}, Reasoning: controls,
			}),
		})
	}
	formats := []struct {
		name      string
		format    canonical.OutputFormat
		specified bool
	}{
		{name: "unspecified"},
		{name: "text", format: textFormat, specified: true},
		{name: "json_object", format: jsonObject, specified: true},
		{name: "json_schema", format: jsonSchema, specified: true},
	}
	reasoningStates := []struct {
		name     string
		controls canonical.ReasoningControls
	}{
		{name: "disabled", controls: disabled},
		{name: "automatic", controls: mustReasoningControls(t, canonical.NewAutomaticReasoningCompute())},
		{name: "budget", controls: reasoning},
	}
	policies := []struct {
		name string
		mode canonical.ToolPolicyMode
		key  *canonical.ToolKey
	}{
		{"none", canonical.ToolPolicyNone, nil},
		{"auto", canonical.ToolPolicyAuto, nil},
		{"required", canonical.ToolPolicyRequired, nil},
		{"specific", canonical.ToolPolicySpecific, &functionKey},
	}
	for _, policy := range policies {
		for _, format := range formats {
			for _, reasoningState := range reasoningStates {
				params := base([]canonical.CanonicalItem{ToolDeclarations(t, function), Message(t, canonical.MessageRoleUser, "generated")})
				params.ToolPolicy = canonical.Specify(canonical.NewToolPolicy(policy.mode, policy.key))
				params.Reasoning = reasoningState.controls
				if format.specified {
					params.OutputFormat = canonical.Specify(format.format)
				}
				witnesses = append(witnesses, ProjectionWitness{
					Name:    fmt.Sprintf("generated policy=%s output=%s reasoning=%s", policy.name, format.name, reasoningState.name),
					Request: canonical.NewCanonicalRequest(params),
				})
			}
		}
	}
	for _, witness := range witnesses {
		if err := canonical.ValidateMaterializedRequest(witness.Request); err != nil {
			t.Fatalf("projection witness %q is invalid: %v", witness.Name, err)
		}
	}
	return witnesses
}

func requestWithOutput(t testing.TB, params canonical.RequestParams, format canonical.OutputFormat) canonical.CanonicalRequest {
	t.Helper()
	params.OutputFormat = canonical.Specify(format)
	return canonical.NewCanonicalRequest(params)
}
func mustReasoningControls(t testing.TB, compute canonical.ReasoningCompute) canonical.ReasoningControls {
	t.Helper()
	controls, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{Compute: canonical.Specify(compute)})
	if err != nil {
		t.Fatal(err)
	}
	return controls
}
