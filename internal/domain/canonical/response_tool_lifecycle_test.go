package canonical

import "testing"

func TestCanonicalResponseWebSearchLifecycleConsumesPendingOccurrences(t *testing.T) {
	call, result := responseWebSearchPair(t, "search_1")
	tests := []struct {
		name    string
		items   []CanonicalItem
		wantErr bool
	}{
		{name: "sequential reuse", items: []CanonicalItem{call, result, call, result}},
		{name: "duplicate pending call", items: []CanonicalItem{call, call, result}, wantErr: true},
		{name: "duplicate result", items: []CanonicalItem{call, result, result}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewCanonicalResponse(
				ResponseRef{SwobuID: NewSwobuResponseID("resp_1")},
				"model",
				test.items,
				Completed("stop"),
				NewUnknownTokenUsage(),
			)
			if test.wantErr && err == nil {
				t.Fatal("canonical response accepted an invalid web-search lifecycle")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("canonical response rejected sequential web-search ID reuse: %v", err)
			}
		})
	}
}

func TestCanonicalResponseSettlementDependsOnlyOnCompletionClass(t *testing.T) {
	call, _ := responseWebSearchPair(t, "search_future")
	tests := []struct {
		name       string
		completion Completion
		wantErr    bool
	}{
		{name: "future incomplete reason", completion: Incomplete("future_interrupted")},
		{name: "future declined reason", completion: Declined("future_policy_stop")},
		{name: "future failed reason", completion: Failed("future_provider_failure")},
		{name: "future completed reason", completion: Completed("future_success"), wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewCanonicalResponse(
				ResponseRef{SwobuID: NewSwobuResponseID("resp_future")},
				"model",
				[]CanonicalItem{call},
				test.completion,
				NewUnknownTokenUsage(),
			)
			if test.wantErr && err == nil {
				t.Fatal("completed response accepted an unsettled provider effect")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("non-completed class required settlement: %v", err)
			}
		})
	}
}

func TestCanonicalResponseDiscoveryLifecycleConsumesPendingOccurrences(t *testing.T) {
	callID, _ := NewToolCallID("discovery_1")
	call, _ := NewToolDiscoveryCallItem(
		callID,
		NewJSONObjectToolInput(mustValidationObject(t, `{}`)),
		DiscoveryExecutorProvider,
	)
	clientCall, _ := NewToolDiscoveryCallItem(
		callID,
		NewJSONObjectToolInput(mustValidationObject(t, `{}`)),
		DiscoveryExecutorClient,
	)
	result, _ := NewToolDiscoveryResultItem(callID, ToolSet{}, DiscoveryExecutorProvider)
	tests := []struct {
		name    string
		items   []CanonicalItem
		wantErr bool
	}{
		{name: "sequential reuse", items: []CanonicalItem{call, result, call, result}},
		{name: "duplicate pending call", items: []CanonicalItem{call, call, result}, wantErr: true},
		{name: "duplicate result", items: []CanonicalItem{call, result, result}, wantErr: true},
		{name: "executor mismatch", items: []CanonicalItem{clientCall, result}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewCanonicalResponse(
				ResponseRef{SwobuID: NewSwobuResponseID("resp_1")},
				"model",
				test.items,
				Completed("stop"),
				NewUnknownTokenUsage(),
			)
			if test.wantErr && err == nil {
				t.Fatal("canonical response accepted an invalid discovery lifecycle")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("canonical response rejected sequential discovery ID reuse: %v", err)
			}
		})
	}
}

func TestCanonicalResponseRejectsCrossVariantDuplicatePendingProviderCall(t *testing.T) {
	webCall, _ := responseWebSearchPair(t, "call_1")
	callID, _ := NewToolCallID("call_1")
	discoveryCall, _ := NewToolDiscoveryCallItem(
		callID,
		NewJSONObjectToolInput(mustValidationObject(t, `{}`)),
		DiscoveryExecutorProvider,
	)
	_, err := NewCanonicalResponse(
		ResponseRef{SwobuID: NewSwobuResponseID("resp_1")},
		"model",
		[]CanonicalItem{webCall, discoveryCall},
		Completed("stop"),
		NewUnknownTokenUsage(),
	)
	if err == nil {
		t.Fatal("canonical response accepted cross-variant duplicate pending provider call ID")
	}
}

func TestCanonicalResponseReservesEveryPendingToolCallID(t *testing.T) {
	function := responseCallableToolCall(t, "call_1", ToolKindFunction, "search")
	custom := responseCallableToolCall(t, "call_1", ToolKindCustom, "shell")
	webCall, webResult := responseWebSearchPair(t, "call_1")
	callID, _ := NewToolCallID("call_1")
	discovery, _ := NewToolDiscoveryCallItem(
		callID,
		NewJSONObjectToolInput(mustValidationObject(t, `{}`)),
		DiscoveryExecutorProvider,
	)
	tests := []struct {
		name  string
		items []CanonicalItem
	}{
		{name: "duplicate function", items: []CanonicalItem{function, function}},
		{name: "duplicate custom", items: []CanonicalItem{custom, custom}},
		{name: "function then web search", items: []CanonicalItem{function, webCall}},
		{name: "custom then discovery", items: []CanonicalItem{custom, discovery}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewCanonicalResponse(
				ResponseRef{SwobuID: NewSwobuResponseID("resp_1")},
				"model",
				test.items,
				Completed("stop"),
				NewUnknownTokenUsage(),
			)
			if err == nil {
				t.Fatal("canonical response accepted a duplicate pending tool-call ID")
			}
		})
	}

	_, err := NewCanonicalResponse(
		ResponseRef{SwobuID: NewSwobuResponseID("resp_1")},
		"model",
		[]CanonicalItem{webCall, webResult, function},
		Completed("stop"),
		NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatalf("canonical response rejected ID reuse after a completed provider pair: %v", err)
	}
}

func TestCanonicalResponseFinalizesPendingEffectsByExecutionOwner(t *testing.T) {
	function := responseCallableToolCall(t, "function_1", ToolKindFunction, "search")
	custom := responseCallableToolCall(t, "custom_1", ToolKindCustom, "shell")
	webCall, _ := responseWebSearchPair(t, "web_1")
	providerDiscovery := responseDiscoveryCall(t, "provider_discovery_1", DiscoveryExecutorProvider)
	clientDiscovery := responseDiscoveryCall(t, "client_discovery_1", DiscoveryExecutorClient)
	tests := []struct {
		name    string
		item    CanonicalItem
		wantErr bool
	}{
		{name: "function remains caller executable", item: function},
		{name: "custom remains caller executable", item: custom},
		{name: "web search requires provider result", item: webCall, wantErr: true},
		{name: "provider discovery requires provider result", item: providerDiscovery, wantErr: true},
		{name: "client discovery remains caller executable", item: clientDiscovery},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewCanonicalResponse(
				ResponseRef{SwobuID: NewSwobuResponseID("resp_1")},
				"model",
				[]CanonicalItem{test.item},
				Completed("stop"),
				NewUnknownTokenUsage(),
			)
			if test.wantErr && err == nil {
				t.Fatal("canonical response accepted an unresolved provider-owned effect")
			}
			if !test.wantErr && err != nil {
				t.Fatalf("canonical response rejected caller-owned pending work: %v", err)
			}
		})
	}
}

func responseCallableToolCall(t *testing.T, id string, kind ToolKind, name string) CanonicalItem {
	t.Helper()
	callID, _ := NewToolCallID(id)
	key, _ := NewRequestToolKey(kind, name)
	var input ToolInput
	switch kind {
	case ToolKindFunction:
		input = NewJSONObjectToolInput(mustValidationObject(t, `{}`))
	case ToolKindCustom:
		input = NewTextToolInput("input")
	default:
		t.Fatalf("unsupported callable tool kind %q", kind)
	}
	call, err := NewToolCallItem(callID, key, input)
	if err != nil {
		t.Fatal(err)
	}
	return call
}

func responseDiscoveryCall(t *testing.T, id string, executor DiscoveryExecutor) CanonicalItem {
	t.Helper()
	callID, _ := NewToolCallID(id)
	call, err := NewToolDiscoveryCallItem(
		callID,
		NewJSONObjectToolInput(mustValidationObject(t, `{}`)),
		executor,
	)
	if err != nil {
		t.Fatal(err)
	}
	return call
}

func responseWebSearchPair(t *testing.T, id string) (CanonicalItem, CanonicalItem) {
	t.Helper()
	callID, _ := NewToolCallID(id)
	input, err := NewWebSearchToolInput(WebSearchCall{Action: WebSearchActionSearch})
	if err != nil {
		t.Fatal(err)
	}
	call, err := NewToolCallItem(callID, WebSearchToolKey(), input)
	if err != nil {
		t.Fatal(err)
	}
	searchResult, err := NewWebSearchResult(nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewWebSearchResultItem(callID, searchResult)
	if err != nil {
		t.Fatal(err)
	}
	return call, result
}
