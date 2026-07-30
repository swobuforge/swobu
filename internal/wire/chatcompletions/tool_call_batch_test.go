package chatcompletions

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestDecodeRequest_DecodesParallelToolCalls(t *testing.T) {
	codec := testClientRequestDecoder{}
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
			name: "true means unspecified",
			raw:  `{"model":"claude","messages":[{"role":"user","content":"hi"}],"parallel_tool_calls":true}`,
			want: canonical.ToolCallBatchUnspecified,
		},
		{
			name: "false lowers to at_most_one",
			raw:  `{"model":"claude","messages":[{"role":"user","content":"hi"}],"parallel_tool_calls":false}`,
			want: canonical.ToolCallBatchAtMostOne,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.ChatCompletions, Raw: []byte(tc.raw)})
			if err != nil {
				t.Fatalf("DecodeClientRequest: %v", err)
			}
			if got.ToolCallBatch().Mode != tc.want {
				t.Fatalf("tool call batch mode = %q, want %q", got.ToolCallBatch().Mode, tc.want)
			}
		})
	}
}

func TestDecodeRequest_RejectsParallelToolCallsWrongType(t *testing.T) {
	codec := testClientRequestDecoder{}
	_, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.ChatCompletions, Raw: []byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}],"parallel_tool_calls":"nope"}`)})
	if err == nil {
		t.Fatal("expected DecodeClientRequest to reject invalid parallel_tool_calls")
	}
}

func TestEncodeCarrier_WiresParallelToolCallsWhenToolsExist(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("claude"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "tool_0"), "search the workspace", canonicaltest.Schema(t, `{"type":"object","properties":{"q":{"type":"string"}}}`), canonical.Unspecified[bool]())),
			canonicaltest.Message(t, canonical.MessageRoleUser, "hi"),
		},
		ToolCallBatch: canonical.Specify(canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne)),
	})
	doc, err := EncodeCarrier(req, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("EncodeCarrier: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(doc.RawBytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	got, ok := payload["parallel_tool_calls"].(bool)
	if !ok || got {
		t.Fatalf("parallel_tool_calls = %#v, want false", payload["parallel_tool_calls"])
	}
}

func TestEncodeCarrier_OmitsParallelToolCallsWithoutTools(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("claude"),
		Items: []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleUser, "hi"),
		},
		ToolCallBatch: canonical.Specify(canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne)),
	})
	doc, err := EncodeCarrier(req, delivery.BufferedDelivery())
	if err != nil {
		t.Fatalf("EncodeCarrier: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(doc.RawBytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if _, ok := payload["parallel_tool_calls"]; ok {
		t.Fatalf("parallel_tool_calls = %#v, want omitted", payload["parallel_tool_calls"])
	}
}
