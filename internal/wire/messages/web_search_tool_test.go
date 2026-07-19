package messages

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestEncodeCarrier_WithWebSearchCapabilityTool(t *testing.T) {
	t.Parallel()

	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "claude-opus-4.8",
		Items: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorUser, "find the latest"),
		},
		Tools: []canonical.ToolDecl{
			canonical.CapabilityToolDecl{
				ID:         canonical.NewSemanticToolID("web_search"),
				Capability: canonical.NewToolCapability("web_search"),
				Config:     canonical.NewToolCapabilityConfigObject(`{"external_web_access":true}`),
				Execution:  canonical.ToolOwnerClient,
			},
		},
	})

	wire, err := EncodeCarrier(req, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("EncodeCarrier returned error: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(wire.Raw, &body); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v", err)
	}
	tools, ok := body["tools"].([]any)
	if !ok {
		t.Fatalf("tools = %T, want []any", body["tools"])
	}
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	tool, ok := tools[0].(map[string]any)
	if !ok {
		t.Fatalf("tool = %T, want map[string]any", tools[0])
	}
	if got := tool["type"]; got != "web_search_20250305" {
		t.Fatalf("tool.type = %v, want web_search_20250305", got)
	}
	if got := tool["name"]; got != "web_search" {
		t.Fatalf("tool.name = %v, want web_search", got)
	}
	if _, ok := tool["input_schema"]; ok {
		t.Fatalf("tool.input_schema = %#v, want omitted for server tool", tool["input_schema"])
	}
}

func TestDecodeResponseBuffered_IgnoresAnthropicServerToolBlocks(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"id":"msg_1",
		"model":"claude-opus-4.8",
		"stop_reason":"end_turn",
		"content":[
			{"type":"text","text":"start"},
			{"type":"server_tool_use","id":"srvtoolu_1","name":"web_search","input":{"query":"hello"}},
			{"type":"web_search_tool_result","tool_use_id":"srvtoolu_1","content":[{"type":"web_search_result","url":"https://example.com","title":"Example","encrypted_content":"enc","page_age":"2025-01-01"}]},
			{"type":"text","text":"end"}
		],
		"usage":{"input_tokens":1,"output_tokens":2}
	}`)

	sink := &recordingDecisionSink{}
	reader, err := decodeResponseBuffered(context.Background(), raw, "ex_web_search", sink)
	if err != nil {
		t.Fatalf("decodeResponseBuffered returned error: %v", err)
	}
	closed, err := canonical.ReadClosedEnvelope(context.Background(), reader, canonical.EnvResponse)
	if err != nil {
		t.Fatalf("ReadClosedEnvelope returned error: %v", err)
	}
	out, err := closed.ProjectResponse()
	if err != nil {
		t.Fatalf("ProjectResponse returned error: %v", err)
	}
	if got := out.Text(); got != "startend" {
		t.Fatalf("out.Text() = %q, want %q", got, "startend")
	}
	if got := len(out.Items()); got != 2 {
		t.Fatalf("out.Items() len = %d, want 2", got)
	}
}

func TestDecodeResponseStream_IgnoresAnthropicServerToolBlocks(t *testing.T) {
	t.Parallel()

	raw := strings.Join([]string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-opus-4.8\"}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"start\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"server_tool_use\",\"id\":\"srvtoolu_1\",\"name\":\"web_search\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"query\\\":\\\"hello\\\"}\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":2,\"content_block\":{\"type\":\"web_search_tool_result\",\"tool_use_id\":\"srvtoolu_1\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":2}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":3,\"content_block\":{\"type\":\"text\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":3,\"delta\":{\"type\":\"text_delta\",\"text\":\"end\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":3}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}, "")

	sink := &recordingDecisionSink{}
	reader := decodeResponseStream(carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex_web_search_stream", sink)
	closed, err := canonical.ReadClosedEnvelope(context.Background(), reader, canonical.EnvResponse)
	if err != nil {
		t.Fatalf("ReadClosedEnvelope returned error: %v", err)
	}
	out, err := closed.ProjectResponse()
	if err != nil {
		t.Fatalf("ProjectResponse returned error: %v", err)
	}
	if got := out.Text(); got != "startend" {
		t.Fatalf("out.Text() = %q, want %q", got, "startend")
	}
	if got := len(out.Items()); got != 2 {
		t.Fatalf("out.Items() len = %d, want 2", got)
	}
}
