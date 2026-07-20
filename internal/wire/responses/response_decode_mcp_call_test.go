package responses

import (
	"context"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"testing"
)

func TestDecodeOutputItemsRejectsUnimplementedMCPCall(t *testing.T) {
	items := []responsesWireOutputItemDTO{{Type: "mcp_call", ID: "mcp_1", Name: "Read", Arguments: `{"path":"workspace/file.txt"}`}}
	if _, err := decodeOutputItems(context.Background(), canonical.CanonicalRequest{}, items, "", "ex", nil); err == nil {
		t.Fatal("MCP call decoded without a canonical branch")
	}
}

func TestDecodeResponseBufferedRejectsUnimplementedMCPCall(t *testing.T) {
	raw := []byte(`{"id":"resp_1","model":"m","output":[{"type":"mcp_call","id":"mcp_1","name":"Read","arguments":"{\"path\":\"workspace/file.txt\"}"}]}`)
	if _, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, raw, "ex", nil); err == nil {
		t.Fatal("MCP call response decoded without a canonical branch")
	}
}
