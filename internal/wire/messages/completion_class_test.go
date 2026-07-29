package messages

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestUnknownStopReasonIsFailed(t *testing.T) {
	completion := messagesCompletion("future_interrupted")
	if completion.Class() != canonical.CompletionFailed || completion.Reason() != "future_interrupted" {
		t.Fatalf("completion = (%q, %q), want failed with original reason", completion.Class(), completion.Reason())
	}
	if got := messagesStopReasonForCompletion(completion, false); got != "future_interrupted" {
		t.Fatalf("projected stop reason = %q, want original reason", got)
	}
}
