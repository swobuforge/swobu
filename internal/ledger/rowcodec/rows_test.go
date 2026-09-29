package rowcodec

import "testing"

func TestInvocationValidateClosedKinds(t *testing.T) {
	if err := (Invocation{Kind: "model", Route: "r", Target: "t", Version: "1", Provider: "p", Protocol: "chat", Model: "m", Purpose: "completion"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Invocation{Kind: "mcp_tool", SourceTool: "s", Tool: "t"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Invocation{Kind: "mcp_tool", Route: "unexpected", SourceTool: "s", Tool: "t"}).Validate(); err == nil {
		t.Fatal("accepted mixed invocation shape")
	}
}
