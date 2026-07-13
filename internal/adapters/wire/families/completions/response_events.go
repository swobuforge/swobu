package completions

import (
	"fmt"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func buildBufferedResponseEvents(exchangeID, resultID, model, text, finishReason string, usage canonical.TokenUsage) []canonical.Event {
	seq := int64(0)
	nextSeq := func() int64 {
		seq++
		return seq
	}
	responseID := canonical.EnvelopeID(fmt.Sprintf("%s:response:0", exchangeID))
	messageID := canonical.EnvelopeID(fmt.Sprintf("%s:message:0", responseID))
	return []canonical.Event{
		{
			ExchangeID: exchangeID,
			Seq:        nextSeq(),
			Time:       time.Now().UTC(),
			Kind:       canonical.EventEnvelopeStart,
			EnvID:      responseID,
			Payload: canonical.EnvelopeStartPayload{
				Kind: canonical.EnvResponse,
			},
		},
		{
			ExchangeID: exchangeID,
			Seq:        nextSeq(),
			Time:       time.Now().UTC(),
			Kind:       canonical.EventMetadata,
			EnvID:      responseID,
			Payload: canonical.MetadataPayload{Values: map[string]string{
				"result_id": resultID,
				"model":     model,
			}},
		},
		{
			ExchangeID: exchangeID,
			Seq:        nextSeq(),
			Time:       time.Now().UTC(),
			Kind:       canonical.EventEnvelopeStart,
			EnvID:      messageID,
			ParentID:   responseID,
			Payload: canonical.EnvelopeStartPayload{
				Kind: canonical.EnvMessage,
				Role: canonical.ItemAuthorAssistant,
			},
		},
		{
			ExchangeID: exchangeID,
			Seq:        nextSeq(),
			Time:       time.Now().UTC(),
			Kind:       canonical.EventTextDelta,
			EnvID:      messageID,
			ParentID:   responseID,
			Payload: canonical.TextDeltaPayload{
				Text: text,
			},
		},
		{
			ExchangeID: exchangeID,
			Seq:        nextSeq(),
			Time:       time.Now().UTC(),
			Kind:       canonical.EventEnvelopeEnd,
			EnvID:      messageID,
			ParentID:   responseID,
			Payload: canonical.EnvelopeEndPayload{
				Kind:   canonical.EnvMessage,
				Status: canonical.EnvelopeStatusCompleted,
			},
		},
		{
			ExchangeID: exchangeID,
			Seq:        nextSeq(),
			Time:       time.Now().UTC(),
			Kind:       canonical.EventUsage,
			EnvID:      responseID,
			Payload: canonical.UsagePayload{
				Usage: usage,
			},
		},
		{
			ExchangeID: exchangeID,
			Seq:        nextSeq(),
			Time:       time.Now().UTC(),
			Kind:       canonical.EventFinish,
			EnvID:      responseID,
			Payload: canonical.FinishPayload{
				Reason: finishReason,
			},
		},
		{
			ExchangeID: exchangeID,
			Seq:        nextSeq(),
			Time:       time.Now().UTC(),
			Kind:       canonical.EventEnvelopeEnd,
			EnvID:      responseID,
			Payload: canonical.EnvelopeEndPayload{
				Kind:   canonical.EnvResponse,
				Status: canonical.EnvelopeStatusCompleted,
			},
		},
	}
}
