package exchange

import (
	"context"
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/session"
	"github.com/swobuforge/swobu/internal/wire"
)

type documentFailureClientCodec struct{ testClientCodec }

func (documentFailureClientCodec) EncodeResponseDocument(canonical.CanonicalRequest, canonical.CanonicalResponse) (wire.ClientDocumentResult, error) {
	return wire.ClientDocumentResult{}, errors.New("forced optional fingerprint projection failure")
}

func TestCheckpointTerminalGateCommitsBeforePublishingFinishWithoutOptionalFingerprint(t *testing.T) {
	response, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: "swobu_gate_order"},
		"m",
		[]canonical.CanonicalItem{testMessage(canonical.MessageRoleAssistant, "ok")},
		canonical.Completed("completed"),
		canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	capture := newCheckpointCaptureResponseStream(canonical.NewSliceEventReader([]canonical.Event{
		{
			ExchangeID: "gate_order", Seq: 1, EnvID: "response",
			Kind: canonical.EventFinish, Payload: canonical.FinishPayload{Completion: canonical.Completed("completed")},
		},
		{
			ExchangeID: "gate_order", Seq: 2, EnvID: "response",
			Kind:    canonical.EventEnvelopeEnd,
			Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted},
		},
	}), canonical.ResponseBinding{})
	capture.result = checkpointCaptureSnapshot{state: checkpointCaptureCompleted, response: response}
	store := session.NewMemoryStore()
	committer := &checkpointCommitter{
		exchangeID: "gate_order", workspaceSlug: "alpha", store: store,
		request: testCanonicalRequest("m"),
	}
	stream := newCheckpointTerminalGate(
		capture,
		documentFailureClientCodec{},
		testCanonicalRequest("m"),
		committer,
	)

	event, err := stream.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if event.Kind != canonical.EventFinish {
		t.Fatalf("first published terminal event = %s, want finish", event.Kind)
	}
	record, found, err := store.Get(context.Background(), "alpha", "swobu_gate_order")
	if err != nil || !found {
		t.Fatalf("checkpoint at finish publication = (%t, %v), want addressable", found, err)
	}
	if record.HistoryFingerprint != nil {
		t.Fatalf("optional history fingerprint = %#v, want absent", record.HistoryFingerprint)
	}
}
