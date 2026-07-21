package responses

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeOutputItemsAllowsOpaqueOnlyMCPCall(t *testing.T) {
	items := []responsesWireOutputItemDTO{{Type: "mcp_call", ID: "mcp_1", Name: "Read", Arguments: `{"path":"workspace/file.txt"}`}}
	decoded, err := decodeOutputItems(context.Background(), canonical.CanonicalRequest{}, items, "", "ex", nil)
	if err != nil || len(decoded) != 0 {
		t.Fatalf("opaque-only MCP projection = %#v, %v", decoded, err)
	}
}

func TestDecodeResponseBufferedAllowsOpaqueOnlyMCPCall(t *testing.T) {
	raw := []byte(`{"id":"resp_1","model":"m","output":[{"type":"mcp_call","id":"mcp_1","name":"Read","arguments":"{\"path\":\"workspace/file.txt\"}"}]}`)
	if _, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, raw, "ex", nil); err != nil {
		t.Fatalf("opaque-only MCP response failed: %v", err)
	}
}
