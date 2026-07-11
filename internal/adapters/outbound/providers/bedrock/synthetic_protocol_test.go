package bedrock

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestBedrockSyntheticProtocol_AllowsOnlyBedrockCatalogProtocols(t *testing.T) {
	t.Parallel()

	output := canonical.NewConversationOutput("resp_1", "m", []canonical.OutputItem{
		canonical.NewTextOutputItem("text_0", "ok"),
	}, "stop")

	for _, kind := range []protocolkind.ProtocolKind{
		protocolkind.ChatCompletions,
		protocolkind.Completions,
	} {
		if _, err := synthesizeBedrockProviderSemanticEventsResult("ex_bedrock", kind, output); err != nil {
			t.Fatalf("kind %s should be supported: %v", kind, err)
		}
	}

	for _, kind := range []protocolkind.ProtocolKind{
		protocolkind.Responses,
		protocolkind.Messages,
	} {
		if _, err := synthesizeBedrockProviderSemanticEventsResult("ex_bedrock", kind, output); err == nil {
			t.Fatalf("kind %s should be rejected", kind)
		}
	}
}

func TestBedrockSyntheticProtocol_ReturnsCanonicalEventStreamIngress(t *testing.T) {
	t.Parallel()

	output := canonical.NewConversationOutput("resp_1", "m", []canonical.OutputItem{
		canonical.NewTextOutputItem("text_0", "ok"),
	}, "stop")

	resp, err := synthesizeBedrockProviderSemanticEventsResult("ex_bedrock", protocolkind.ChatCompletions, output)
	if err != nil {
		t.Fatalf("event synth error: %v", err)
	}
	eventsCarrier, ok := resp.(carrier.CanonicalEventStream)
	if !ok {
		t.Fatalf("carrier type = %T", resp)
	}
	ev, err := eventsCarrier.Events.Next(context.Background())
	if err != nil {
		t.Fatalf("read first event: %v", err)
	}
	if ev.ExchangeID != "ex_bedrock" {
		t.Fatalf("exchange_id=%q want ex_bedrock", ev.ExchangeID)
	}
	if err := eventsCarrier.Events.Close(context.Background()); err != nil {
		t.Fatalf("close events: %v", err)
	}
}
