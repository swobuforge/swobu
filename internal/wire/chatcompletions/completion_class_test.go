package chatcompletions

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestUnknownFinishReasonIsFailed(t *testing.T) {
	completion := chatCompletion("future_interrupted")
	if completion.Class() != canonical.CompletionFailed || completion.Reason() != "future_interrupted" {
		t.Fatalf("completion = (%q, %q), want failed with original reason", completion.Class(), completion.Reason())
	}
}
