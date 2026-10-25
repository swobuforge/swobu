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

func TestInvocationRejectsPartialAndForeignConnectionBinding(t *testing.T) {
	base := Invocation{Kind: "model", Route: "r", Target: "t", Version: "1", Provider: "p", Protocol: "responses", Model: "m", Purpose: "exchange_attempt"}
	for _, pair := range [][2]string{{"cn_a", ""}, {"", "1"}, {"INVALID", "1"}, {"cn_a", "0"}, {"cn_a", "01"}, {"cn_a", "18446744073709551616"}} {
		row := base
		row.ConnectionID, row.ConnectionRevision = pair[0], pair[1]
		if row.Validate() == nil {
			t.Fatal("accepted malformed composite identity")
		}
	}
	base.ConnectionID, base.ConnectionRevision = "cn_a", "18446744073709551615"
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	base.Kind = "mcp_tool"
	if base.Validate() == nil {
		t.Fatal("MCP acquired model Connection binding")
	}
}
