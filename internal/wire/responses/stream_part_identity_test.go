package responses

import (
	"encoding/json"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func TestResponseStreamWireEncoderPreservesContentPartIndexes(t *testing.T) {
	encoder := NewResponseStreamWireEncoder()
	events := []sse.StreamEvent{
		{Kind: sse.StreamEventStarted, ResultID: "resp_1"},
		{Kind: sse.StreamEventItemStarted, ItemID: "item_0", ItemOrdinal: 0, ItemKind: canonical.ItemKindMessage},
		{Kind: sse.StreamEventContentStarted, ItemID: "item_0", PartOrdinal: 0, PartKind: canonical.PartKindText},
		{Kind: sse.StreamEventTextDelta, ItemID: "item_0", PartOrdinal: 0, TextDelta: "first"},
		{Kind: sse.StreamEventContentStarted, ItemID: "item_0", PartOrdinal: 1, PartKind: canonical.PartKindText},
		{Kind: sse.StreamEventTextDelta, ItemID: "item_0", PartOrdinal: 1, TextDelta: "second"},
	}
	seen := map[int]bool{}
	for _, event := range events {
		frames, err := encoder.Encode(event)
		if err != nil {
			t.Fatal(err)
		}
		for _, frame := range frames {
			var payload map[string]any
			if json.Unmarshal(frame, &payload) == nil && payload["type"] == "response.output_text.delta" {
				seen[int(payload["content_index"].(float64))] = true
			}
		}
	}
	if !seen[0] || !seen[1] || len(seen) != 2 {
		t.Fatalf("delta content indexes = %#v, want 0 and 1", seen)
	}
}
