package messages

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestDecodeRequest_DecodesDisableParallelToolUse(t *testing.T) {
	codec := ClientRequestDecoder{}
	cases := []struct {
		name string
		raw  string
		want canonical.ToolCallBatchMode
	}{
		{
			name: "omitted",
			raw:  `{"model":"claude","messages":[{"role":"user","content":"hi"}]}`,
			want: canonical.ToolCallBatchUnspecified,
		},
		{
			name: "false means unspecified",
			raw:  `{"model":"claude","messages":[{"role":"user","content":"hi"}],"disable_parallel_tool_use":false}`,
			want: canonical.ToolCallBatchUnspecified,
		},
		{
			name: "true lowers to at_most_one",
			raw:  `{"model":"claude","messages":[{"role":"user","content":"hi"}],"disable_parallel_tool_use":true}`,
			want: canonical.ToolCallBatchAtMostOne,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := codec.DecodeClientRequest(carrier.WireDocument{Family: protocolkind.Messages, Raw: []byte(tc.raw)})
			if err != nil {
				t.Fatalf("DecodeClientRequest: %v", err)
			}
			if got.ToolCallBatch().Mode != tc.want {
				t.Fatalf("tool call batch mode = %q, want %q", got.ToolCallBatch().Mode, tc.want)
			}
		})
	}
}

func TestDecodeRequest_RejectsDisableParallelToolUseWrongType(t *testing.T) {
	codec := ClientRequestDecoder{}
	_, _, err := codec.DecodeClientRequest(carrier.WireDocument{Family: protocolkind.Messages, Raw: []byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}],"disable_parallel_tool_use":"nope"}`)})
	if err == nil {
		t.Fatal("expected DecodeClientRequest to reject invalid disable_parallel_tool_use")
	}
}

func TestEncodeCarrier_WiresDisableParallelToolUseWhenToolsExist(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "claude",
		Items: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorUser, "hi"),
		},
		Tools: []canonical.ToolDecl{
			canonical.NewFunctionToolDecl("tool_0", "search", "search the workspace", canonical.NewToolSchemaObject(`{"type":"object","properties":{"q":{"type":"string"}}}`)),
		},
		ToolCallBatch: canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne),
	})
	doc, err := EncodeCarrier(req, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("EncodeCarrier: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(doc.RawBytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	got, ok := payload["disable_parallel_tool_use"].(bool)
	if !ok || !got {
		t.Fatalf("disable_parallel_tool_use = %#v, want true", payload["disable_parallel_tool_use"])
	}
}

func TestEncodeCarrier_OmitsDisableParallelToolUseWithoutTools(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "claude",
		Items: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorUser, "hi"),
		},
		ToolCallBatch: canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne),
	})
	doc, err := EncodeCarrier(req, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("EncodeCarrier: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(doc.RawBytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if _, ok := payload["disable_parallel_tool_use"]; ok {
		t.Fatalf("disable_parallel_tool_use = %#v, want omitted", payload["disable_parallel_tool_use"])
	}
}
