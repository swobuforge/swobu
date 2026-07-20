package messages

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestDecodeRequest_DecodesDisableParallelToolUse(t *testing.T) {
	codec := legacyClientRequestDecoder{}
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
			got, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.Messages, Raw: []byte(tc.raw)})
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
	codec := legacyClientRequestDecoder{}
	_, _, err := codec.DecodeClientRequest(carrier.Document{Family: protocolkind.Messages, Raw: []byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}],"disable_parallel_tool_use":"nope"}`)})
	if err == nil {
		t.Fatal("expected DecodeClientRequest to reject invalid disable_parallel_tool_use")
	}
}

func TestEncodeCarrier_WiresDisableParallelToolUseWhenToolsExist(t *testing.T) {
	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("claude"),
		Items: []canonical.CanonicalItem{
			canonicaltest.Message(t, canonical.MessageRoleUser, "hi"),
		},
		Tools:         canonicaltest.SpecifiedToolSet(t, canonicaltest.MustFunctionTool(canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "tool_0"), "search the workspace", canonicaltest.Schema(t, `{"type":"object","properties":{"q":{"type":"string"}}}`), canonical.Unspecified[bool]())),
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
	toolChoice, ok := payload["tool_choice"].(map[string]any)
	if !ok {
		t.Fatalf("tool_choice = %#v, want object", payload["tool_choice"])
	}
	got, ok := toolChoice["disable_parallel_tool_use"].(bool)
	if !ok || !got {
		t.Fatalf("tool_choice.disable_parallel_tool_use = %#v, want true", toolChoice["disable_parallel_tool_use"])
	}
}

func TestEncodeCarrier_OmitsDisableParallelToolUseWithoutTools(t *testing.T) {
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
	if _, ok := payload["disable_parallel_tool_use"]; ok {
		t.Fatalf("disable_parallel_tool_use = %#v, want omitted", payload["disable_parallel_tool_use"])
	}
	if _, ok := payload["tool_choice"]; ok {
		t.Fatalf("tool_choice = %#v, want omitted", payload["tool_choice"])
	}
}

func TestEncodeCarrier_SynthesizesAutoToolChoiceWhenAbsent(t *testing.T) {
	got, err := encodeMessagesToolCallBatch(nil, canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne), true)
	if err != nil {
		t.Fatalf("encodeMessagesToolCallBatch: %v", err)
	}
	payload, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("result = %#v, want map", got)
	}
	if got, want := payload["type"], "auto"; got != want {
		t.Fatalf("tool_choice.type = %q, want %q", got, want)
	}
	if got, ok := payload["disable_parallel_tool_use"].(bool); !ok || !got {
		t.Fatalf("tool_choice.disable_parallel_tool_use = %#v, want true", payload["disable_parallel_tool_use"])
	}
}
