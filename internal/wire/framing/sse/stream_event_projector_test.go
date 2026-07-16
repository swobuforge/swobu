package sse

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestEnvelopeEventAdapter_ErrorMapsToFailedEvent(t *testing.T) {
	t.Parallel()

	adapter := NewEnvelopeEventAdapter()
	started := adapter.Translate(canonical.Event{
		Kind:  canonical.EventEnvelopeStart,
		EnvID: "resp_1",
		Payload: canonical.EnvelopeStartPayload{
			Kind: canonical.EnvResponse,
		},
	})
	if len(started) != 1 || started[0].Kind != StreamEventStarted {
		t.Fatalf("started events = %#v, want one started event", started)
	}

	failed := adapter.Translate(canonical.Event{
		Kind:  canonical.EventError,
		EnvID: "resp_1",
		Payload: canonical.ErrorPayload{
			Code:    "stream_unexpected_eof",
			Message: "output stream ended before completed",
		},
	})
	if len(failed) != 1 {
		t.Fatalf("failed events len = %d, want 1: %#v", len(failed), failed)
	}
	if failed[0].Kind != StreamEventFailed {
		t.Fatalf("failed event kind = %q, want %q", failed[0].Kind, StreamEventFailed)
	}
	if failed[0].ErrorCode != "stream_unexpected_eof" {
		t.Fatalf("failed error code = %q", failed[0].ErrorCode)
	}
	if failed[0].ErrorMessage != "output stream ended before completed" {
		t.Fatalf("failed error message = %q", failed[0].ErrorMessage)
	}

	trailingEnd := adapter.Translate(canonical.Event{
		Kind:  canonical.EventEnvelopeEnd,
		EnvID: "resp_1",
		Payload: canonical.EnvelopeEndPayload{
			Kind:   canonical.EnvResponse,
			Status: canonical.EnvelopeStatusError,
		},
	})
	if len(trailingEnd) != 0 {
		t.Fatalf("trailing error envelope end emitted %#v, want no success completion", trailingEnd)
	}
}

func TestEnvelopeEventAdapter_ErrorEnvelopeEndMapsToFailedEvent(t *testing.T) {
	t.Parallel()

	adapter := NewEnvelopeEventAdapter()
	_ = adapter.Translate(canonical.Event{
		Kind:  canonical.EventEnvelopeStart,
		EnvID: "resp_1",
		Payload: canonical.EnvelopeStartPayload{
			Kind: canonical.EnvResponse,
		},
	})

	failed := adapter.Translate(canonical.Event{
		Kind:  canonical.EventEnvelopeEnd,
		EnvID: "resp_1",
		Payload: canonical.EnvelopeEndPayload{
			Kind:   canonical.EnvResponse,
			Status: canonical.EnvelopeStatusError,
		},
	})
	if len(failed) != 1 || failed[0].Kind != StreamEventFailed {
		t.Fatalf("error envelope end emitted %#v, want one failed event", failed)
	}
	if failed[0].ErrorCode != "stream_error" {
		t.Fatalf("default error code = %q, want stream_error", failed[0].ErrorCode)
	}
}

func TestEnvelopeEventAdapter_UsageDoesNotCompleteBeforeResponseEnd(t *testing.T) {
	t.Parallel()

	adapter := NewEnvelopeEventAdapter()
	_ = adapter.Translate(canonical.Event{
		Kind:  canonical.EventEnvelopeStart,
		EnvID: "resp_1",
		Payload: canonical.EnvelopeStartPayload{
			Kind: canonical.EnvResponse,
		},
	})

	usageEvents := adapter.Translate(canonical.Event{
		Kind:  canonical.EventUsage,
		EnvID: "resp_1",
		Payload: canonical.UsagePayload{
			Usage: canonical.NewUnknownTokenUsage(),
		},
	})
	if len(usageEvents) != 0 {
		t.Fatalf("usage emitted terminal events %#v, want none", usageEvents)
	}

	finishEvents := adapter.Translate(canonical.Event{
		Kind:  canonical.EventFinish,
		EnvID: "resp_1",
		Payload: canonical.FinishPayload{
			Reason: "stop",
		},
	})
	if len(finishEvents) != 0 {
		t.Fatalf("finish emitted terminal events %#v, want none before response end", finishEvents)
	}

	completed := adapter.Translate(canonical.Event{
		Kind:  canonical.EventEnvelopeEnd,
		EnvID: "resp_1",
		Payload: canonical.EnvelopeEndPayload{
			Kind:   canonical.EnvResponse,
			Status: canonical.EnvelopeStatusCompleted,
		},
	})
	if len(completed) != 1 || completed[0].Kind != StreamEventCompleted {
		t.Fatalf("response end emitted %#v, want one completed event", completed)
	}
	if completed[0].FinishReason != "stop" {
		t.Fatalf("finish reason = %q, want stop", completed[0].FinishReason)
	}
}

func TestEnvelopeEventAdapter_ResponseStartIgnoresNativeID(t *testing.T) {
	t.Parallel()

	adapter := NewEnvelopeEventAdapter()
	started := adapter.Translate(canonical.Event{
		Kind:  canonical.EventEnvelopeStart,
		EnvID: "resp_1",
		Meta:  canonical.EventMetadataFields{NativeID: "resp_swobu_alloc"},
		Payload: canonical.EnvelopeStartPayload{
			Kind: canonical.EnvResponse,
		},
	})
	if len(started) != 1 || started[0].Kind != StreamEventStarted {
		t.Fatalf("started events = %#v, want one started event", started)
	}
	if started[0].ResultID != "" {
		t.Fatalf("started result id = %q, want empty when ResultID is absent", started[0].ResultID)
	}

	completed := adapter.Translate(canonical.Event{
		Kind:  canonical.EventEnvelopeEnd,
		EnvID: "resp_1",
		Payload: canonical.EnvelopeEndPayload{
			Kind:   canonical.EnvResponse,
			Status: canonical.EnvelopeStatusCompleted,
		},
	})
	if len(completed) != 1 || completed[0].Kind != StreamEventCompleted {
		t.Fatalf("completed events = %#v, want one completed event", completed)
	}
	if completed[0].ResultID != "" {
		t.Fatalf("completed result id = %q, want empty when ResultID is absent", completed[0].ResultID)
	}
}

func TestEnvelopeEventAdapter_ResponseStartPrefersResultID(t *testing.T) {
	t.Parallel()

	adapter := NewEnvelopeEventAdapter()
	started := adapter.Translate(canonical.Event{
		Kind:  canonical.EventEnvelopeStart,
		EnvID: "resp_2",
		Meta: canonical.EventMetadataFields{
			NativeID: "provider_resp_2",
			ResultID: "swobu_resp_2",
		},
		Payload: canonical.EnvelopeStartPayload{
			Kind: canonical.EnvResponse,
		},
	})
	if len(started) != 1 || started[0].Kind != StreamEventStarted {
		t.Fatalf("started events = %#v, want one started event", started)
	}
	if started[0].ResultID != "swobu_resp_2" {
		t.Fatalf("started result id = %q, want swobu_resp_2", started[0].ResultID)
	}

	completed := adapter.Translate(canonical.Event{
		Kind:  canonical.EventEnvelopeEnd,
		EnvID: "resp_2",
		Payload: canonical.EnvelopeEndPayload{
			Kind:   canonical.EnvResponse,
			Status: canonical.EnvelopeStatusCompleted,
		},
	})
	if len(completed) != 1 || completed[0].Kind != StreamEventCompleted {
		t.Fatalf("completed events = %#v, want one completed event", completed)
	}
	if completed[0].ResultID != "swobu_resp_2" {
		t.Fatalf("completed result id = %q, want swobu_resp_2", completed[0].ResultID)
	}
}
