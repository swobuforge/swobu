package exchange

import (
	"context"
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/continuity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
)

type documentFailureClientCodec struct{ testClientCodec }

func (documentFailureClientCodec) EncodeResponseDocument(canonical.CanonicalRequest, canonical.CanonicalResponse) (wire.ClientDocumentResult, error) {
	return wire.ClientDocumentResult{}, errors.New("forced optional fingerprint projection failure")
}

type responseRecordingClientCodec struct {
	testClientCodec
	encoded canonical.CanonicalResponse
}

func (c *responseRecordingClientCodec) EncodeResponseDocument(request canonical.CanonicalRequest, response canonical.CanonicalResponse) (wire.ClientDocumentResult, error) {
	c.encoded = response.Clone()
	return c.testClientCodec.EncodeResponseDocument(request, response)
}

func TestCheckpointTerminalGateFingerprintsClientVisibleResponseBeforeCheckpointRefinement(t *testing.T) {
	live, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: "swobu_live_fingerprint"},
		"m",
		[]canonical.CanonicalItem{testMessage(canonical.MessageRoleAssistant, "answer")},
		canonical.Completed("completed"),
		canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	summary, _ := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "checkpoint only")
	reasoning, _ := canonical.NewReasoningItem([]canonical.ReasoningPart{summary}, canonical.OpaqueThinking{})
	checkpoint, err := live.WithReasoningPrelude(reasoning)
	if err != nil {
		t.Fatal(err)
	}
	events := canonical.SynthesizeResponseEnvelopeEvents("live_fingerprint", live.Response(), live.Model(), live.Items(), live.Completion(), live.Usage())
	capture := newCheckpointCaptureResponseStream(canonical.NewSliceEventReader(events), canonical.ResponseBinding{SwobuID: live.Response().SwobuID}, func(canonical.CanonicalResponse) (canonical.CanonicalResponse, error) {
		return checkpoint, nil
	})
	codec := &responseRecordingClientCodec{}
	stream := newCheckpointTerminalGate(capture, codec, testCanonicalRequest("m"), &checkpointCommitter{
		exchangeID: "live_fingerprint", workspaceSlug: "alpha", store: continuity.NewMemoryStore(),
		request: testCanonicalRequest("m"), executionAffinity: testExecutionAffinity("live-fingerprint"),
	})
	for len(codec.encoded.Items()) == 0 {
		if _, err := stream.Next(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	items := codec.encoded.Items()
	if len(items) != 1 || items[0].Kind() != canonical.ItemKindMessage {
		t.Fatalf("fingerprinted response items = %#v, want client-visible message only", items)
	}
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
	capture.result = checkpointCaptureSnapshot{state: checkpointCaptureCompleted, clientResponse: response, checkpointResponse: response}
	store := continuity.NewMemoryStore()
	committer := &checkpointCommitter{
		exchangeID: "gate_order", workspaceSlug: "alpha", store: store,
		request:           testCanonicalRequest("m"),
		executionAffinity: testExecutionAffinity("gate-order"),
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
	if record.History != nil {
		t.Fatalf("optional history fingerprint = %#v, want absent", record.History)
	}
	if record.ExecutionAffinity != testExecutionAffinity("gate-order") {
		t.Fatal("checkpoint did not retain its resolved execution affinity")
	}
}

func TestCheckpointCaptureRejectsNativeHandleForAnotherTarget(t *testing.T) {
	for _, tc := range []struct {
		name string
		ref  canonical.ResponseRef
	}{
		{
			name: "Responses",
			ref: canonical.ResponseRef{SwobuID: "resp_capture", Responses: &canonical.ResponsesContinuation{
				ProviderResponseID: "provider_response", TargetID: "other-target", TargetVersion: 1,
			}},
		},
		{
			name: "Gemini Interactions",
			ref: canonical.ResponseRef{SwobuID: "resp_capture", Interactions: &canonical.InteractionsContinuation{
				ProviderInteractionID: "interaction_provider", TargetID: "other-target", TargetVersion: 1,
			}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capture := newCheckpointCaptureResponseStream(canonical.NewSliceEventReader([]canonical.Event{{
				Kind: canonical.EventResponseIdentity, Payload: canonical.ResponseIdentityPayload{Response: tc.ref},
			}}), canonical.ResponseBinding{SwobuID: "resp_capture", TargetID: "attempted-target", TargetVersion: 1})
			if _, err := capture.Next(context.Background()); err == nil {
				t.Fatal("checkpoint capture accepted a native handle for another target")
			}
		})
	}
}
