package exchange

import (
	"context"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

const deltaChunk = "benchmark-stream-chunk-payload"

// responseIdentity is the binding every test response carries. The capture
// stream's Next() validates EventResponseIdentity against it, so the synthetic
// identity must match for the stream to reach terminal success.
var responseIdentity = canonical.ResponseBinding{SwobuID: canonical.NewSwobuResponseID("resp_bench")}

// syntheticResponseEvents builds a production-shaped completed-response event
// stream whose length scales with deltaCount: one assistant message item with a
// single text part fed by deltaCount text deltas, then a completed checkpoint,
// then usage/finish/envelope-end(completed).
//
// Events are wrapped exactly the way the real wire decoders do (see
// wire/responses/response_stream.go: enqueueTextDelta / stageOutputEvent): each
// item event carries canonical.ItemEvent{Position, Payload}. Using bare
// TextDeltaPayload here would fail the ItemEvent assertion in projection and
// silently make the fold a no-op, so the wrapping is load-bearing for realism.
func syntheticResponseEvents(deltaCount int) []canonical.Event {
	start, err := canonical.NewMessageStart(canonical.MessageRoleAssistant)
	if err != nil {
		panic(err)
	}
	events := make([]canonical.Event, 0, deltaCount+6)
	seq := int64(0)
	next := func(kind canonical.EventKind, payload any) canonical.Event {
		seq++
		return canonical.Event{Seq: seq, Kind: kind, Payload: payload, EnvID: "resp_bench:response:0"}
	}
	events = append(events,
		next(canonical.EventEnvelopeStart, canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse, Model: "bench-model"}),
		next(canonical.EventResponseIdentity, canonical.ResponseIdentityPayload{Response: canonical.ResponseRef{SwobuID: responseIdentity.SwobuID}}),
		next(canonical.EventItemStart, canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0}, Payload: start}),
		next(canonical.EventContentStart, canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0, Part: 0}, Payload: canonical.NewMessageContentStart(canonical.PartKindText)}),
	)
	for range deltaCount {
		events = append(events, next(canonical.EventTextDelta, canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0, Part: 0}, Payload: canonical.TextDeltaPayload{Text: deltaChunk}}))
	}
	// The completed checkpoint's text part must equal the concatenated deltas;
	// the item-stream assembler validates this at EventItemCompleted.
	completed, err := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart(strings.Repeat(deltaChunk, deltaCount))})
	if err != nil {
		panic(err)
	}
	events = append(events,
		next(canonical.EventItemCompleted, canonical.ItemEvent{Position: canonical.ItemPosition{Item: 0}, Payload: canonical.ItemCompletedPayload{Item: completed}}),
		next(canonical.EventUsage, canonical.UsagePayload{}),
		next(canonical.EventFinish, canonical.FinishPayload{}),
		next(canonical.EventEnvelopeEnd, canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}),
	)
	return events
}

// drainCapture feeds events through one capture stream until it reaches the
// terminal completed event, mirroring what a real streaming handoff does. It
// returns the projected terminal snapshot the stream would checkpoint.
func drainCapture(tb testing.TB, events []canonical.Event) checkpointCaptureSnapshot {
	tb.Helper()
	capture := newCheckpointCaptureResponseStream(canonical.NewSliceEventReader(events), responseIdentity)
	ctx := context.Background()
	for {
		event, err := capture.Next(ctx)
		if err != nil {
			tb.Fatalf("unexpected stream error after event %d (%s): %v", event.Seq, event.Kind, err)
		}
		if capture.result.state != checkpointCapturePending {
			break
		}
	}
	_ = capture.Close(ctx)
	return capture.snapshot()
}

func BenchmarkCheckpointCaptureStream(b *testing.B) {
	for _, deltaCount := range []int{16, 512, 4096} {
		events := syntheticResponseEvents(deltaCount)
		b.Run(nameForDeltaCount(deltaCount), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				drainCapture(b, events)
			}
		})
	}
}

func nameForDeltaCount(n int) string {
	switch n {
	case 16:
		return "Short"
	case 512:
		return "Medium"
	default:
		return "Long"
	}
}
