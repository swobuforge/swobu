package wire

import (
	"context"
	"errors"
	"testing"
)

func TestStageResponseFailurePreservesFirstBoundaryAndCancellation(t *testing.T) {
	cause := errors.New("provider bytes invalid")
	provider := StageResponseFailure("provider_stream_decode", cause)
	later := StageResponseFailure("canonical_response_validation", provider)
	stage, ok := ResponseFailureStage(later)
	if !ok || stage != "provider_stream_decode" {
		t.Fatalf("stage = %q, ok=%v, want first boundary", stage, ok)
	}
	if ResponseFailureCause(later) != cause {
		t.Fatalf("cause = %v, want original cause", ResponseFailureCause(later))
	}

	canceled := StageResponseFailure("client_stream_encode", context.Canceled)
	if !errors.Is(canceled, context.Canceled) {
		t.Fatalf("canceled = %v", canceled)
	}
	if stage, ok := ResponseFailureStage(canceled); ok || stage != "" {
		t.Fatalf("cancellation acquired stage %q", stage)
	}
}
