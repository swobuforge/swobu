package responses

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestUnknownTerminalStatusIsFailed(t *testing.T) {
	completion := responsesCompletion("future_interrupted", "future_detail")
	if completion.Class() != canonical.CompletionFailed || completion.Reason() != "future_detail" {
		t.Fatalf("completion = (%q, %q), want failed with original reason", completion.Class(), completion.Reason())
	}
	status, reason := responsesWireStatusForCompletion(completion)
	if status != "incomplete" || reason != "future_detail" {
		t.Fatalf("projected terminal = (%q, %q), want incomplete with original reason", status, reason)
	}

	status, reason = responsesWireStatusForCompletion(canonical.Completed("future_success"))
	if status != "completed" || reason != "" {
		t.Fatalf("completed projection = (%q, %q), want completed without incomplete reason", status, reason)
	}
}
