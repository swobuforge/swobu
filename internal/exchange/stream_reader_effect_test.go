package exchange

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
)

type effectRecordingSink struct {
	effects []effect.Effect
}

func (s *effectRecordingSink) Commit(_ context.Context, _ string, effects []effect.Effect) error {
	s.effects = append(s.effects, effects...)
	return nil
}

type readerWithEffects struct {
	canonical.EventReader
	effects []effect.Effect
}

func (r readerWithEffects) Effects() []effect.Effect {
	return append([]effect.Effect(nil), r.effects...)
}

func TestStreamingCommitsReaderEffects(t *testing.T) {
	sink := &effectRecordingSink{}
	reader := readerWithEffects{
		EventReader: canonical.NewSliceEventReader([]canonical.Event{
			{
				Kind:  canonical.EventEnvelopeStart,
				EnvID: "r1",
				Payload: canonical.EnvelopeStartPayload{
					Kind: canonical.EnvResponse,
				},
			},
			{
				Kind:  canonical.EventEnvelopeEnd,
				EnvID: "r1",
				Payload: canonical.EnvelopeEndPayload{
					Kind:   canonical.EnvResponse,
					Status: canonical.EnvelopeStatusCompleted,
				},
			},
		}),
		effects: []effect.Effect{
			effect.TurnStateEffect{
				Op:    effect.TurnStateReplay,
				Key:   "turn.request.raw",
				Value: []byte("cached-raw"),
			},
		},
	}

	in := ExchangeInput{
		ExchangeID:       "ex_reader_effects",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.StreamingDelivery(delivery.FramingSSE),
		Request:          testCanonicalRequest("m"),
		ReplayScope:      unsafeLocalReplayScope("alpha"),
		Target:           NewRoutableTarget("backend-a", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "responses"),
		Contract:         NewExecutionContract(delivery.StreamingDelivery(delivery.FramingSSE)),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
	}

	out, err := encodeClientOutput(context.Background(), in, testClientCodec{}, reader, true, sink)
	if err != nil {
		t.Fatalf("encodeClientOutput returned error: %v", err)
	}
	if out.Transport.Body == nil {
		t.Fatal("streaming transport body was nil")
	}

	found := false
	for _, eff := range sink.effects {
		turnState, ok := eff.(effect.TurnStateEffect)
		if !ok {
			continue
		}
		if string(turnState.Key) == "turn.request.raw" {
			found = true
			if string(turnState.Value) != "cached-raw" {
				t.Fatalf("turn-state value = %q, want cached-raw", string(turnState.Value))
			}
		}
	}
	if !found {
		t.Fatal("streaming reader effects were not committed")
	}
}
