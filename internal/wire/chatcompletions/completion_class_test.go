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
	if _, err := chatClientFinishReason(completion, false); err == nil {
		t.Fatal("failed completion projected as Chat success")
	}
}

func TestChatTerminalProjectionPrecedence(t *testing.T) {
	tests := []struct {
		name       string
		completion canonical.Completion
		hasTools   bool
		want       string
		wantErr    bool
	}{
		{name: "completed tool call", completion: canonical.Completed("completed"), hasTools: true, want: "tool_calls"},
		{name: "incomplete with tool call", completion: canonical.Incomplete("length"), hasTools: true, want: "length"},
		{name: "declined with tool call", completion: canonical.Declined("safety"), hasTools: true, want: "content_filter"},
		{name: "failed with tool call", completion: canonical.Failed("interrupted"), hasTools: true, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := chatClientFinishReason(test.completion, test.hasTools)
			if test.wantErr {
				if err == nil {
					t.Fatalf("finish reason = %q, want projection error", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("finish reason = %q, want %q", got, test.want)
			}
		})
	}
}
