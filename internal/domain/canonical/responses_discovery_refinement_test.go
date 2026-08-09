package canonical

import "testing"

func TestResponsesNullDiscoveryCallIDRequiresProviderExecution(t *testing.T) {
	callID, _ := NewToolCallID("call_1")
	object, _ := ParseJSONObject([]byte(`{}`))
	input := NewJSONObjectToolInput(object)
	if _, err := NewToolDiscoveryCallItemWithResponses(callID, input, DiscoveryExecutorClient, true); err == nil {
		t.Fatal("client discovery call accepted a null Responses wire call id")
	}
	tools, _ := NewToolSet(nil)
	if _, err := NewToolDiscoveryResultItemWithVisibilityWireID(callID, tools, DiscoveryExecutorClient, ToolVisibilityRefinements{}, true); err == nil {
		t.Fatal("client discovery result accepted a null Responses wire call id")
	}
}
