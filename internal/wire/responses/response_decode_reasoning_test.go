package responses

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type recordingDecisionSink struct {
	effects []compat.Decision
}

func (s *recordingDecisionSink) Commit(_ context.Context, _ string, effects []compat.Decision) error {
	s.effects = append(s.effects, effects...)
	return nil
}

func TestDecodeResponseBuffered_RejectsReasoningOutput(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"id":"resp_1",
		"model":"m",
		"output":[{"type":"reasoning","id":"reasoning_1"}]
	}`)
	sink := &recordingDecisionSink{}

	_, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, raw, "ex_reasoning", sink)
	if err == nil {
		t.Fatal("expected decodeResponseBuffered to reject reasoning output")
	}
	var compatErr canonical.Error
	if !errors.As(err, &compatErr) {
		t.Fatalf("expected canonical.Error, got %T", err)
	}
	if compatErr.Code != canonical.ErrorCodeUnsupportedOperation {
		t.Fatalf("error code = %q, want %q", compatErr.Code, canonical.ErrorCodeUnsupportedOperation)
	}
	if !strings.Contains(compatErr.Message, "reasoning") {
		t.Fatalf("error message = %q, want reasoning to be mentioned", compatErr.Message)
	}
	if len(sink.effects) != 0 {
		t.Fatalf("captured effects = %#v, want none until canonical Thinking exists", sink.effects)
	}
}
