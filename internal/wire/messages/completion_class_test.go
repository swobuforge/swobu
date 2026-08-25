package messages

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestUnknownStopReasonIsFailed(t *testing.T) {
	completion := messagesCompletion("future_interrupted")
	if completion.Class() != canonical.CompletionFailed || completion.Reason() != "future_interrupted" {
		t.Fatalf("completion = (%q, %q), want failed with original reason", completion.Class(), completion.Reason())
	}
	if _, err := messagesStopReasonForCompletion(completion, false); err == nil {
		t.Fatal("failed completion projected as Messages success")
	}
}

func TestMessagesSuccessfulTerminalProjectionIgnoresProviderVocabulary(t *testing.T) {
	message := canonicaltest.Message(t, canonical.MessageRoleAssistant, "done")
	for _, providerReason := range []string{"completed", "stop", "end_turn", "whatever-provider-called-it"} {
		t.Run(providerReason, func(t *testing.T) {
			response := canonicaltest.Response(t, "resp_1", "m", []canonical.CanonicalItem{message}, canonical.Completed(providerReason))
			encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(encoded.Document.RawBytes(), []byte(`"stop_reason":"end_turn"`)) {
				t.Fatalf("buffered terminal = %s, want end_turn", encoded.Document.RawBytes())
			}
			events := canonical.NewSliceEventReader(canonical.SynthesizeResponseEnvelopeEvents("exchange", response.Response(), response.Model(), response.Items(), response.Completion(), response.Usage()))
			streamed, err := (ResponseStreamEncoder{}).EncodeResponseStream(context.Background(), canonical.CanonicalRequest{}, events, delivery.StreamingDelivery(delivery.FramingSSE))
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(streamed.Stream.Body)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(raw, []byte(`"stop_reason":"end_turn"`)) {
				t.Fatalf("stream terminal = %s, want end_turn", raw)
			}
		})
	}
}

func TestMessagesToolUseAndIncompleteTerminalProjection(t *testing.T) {
	if got, err := messagesStopReasonForCompletion(canonical.Completed("completed"), true); err != nil || got != "tool_use" {
		t.Fatalf("tool terminal = %q, want tool_use", got)
	}
	if got, err := messagesStopReasonForCompletion(canonical.Incomplete("length"), false); err != nil || got != "max_tokens" {
		t.Fatalf("incomplete terminal = %q, want max_tokens", got)
	}
	if got, err := messagesStopReasonForCompletion(canonical.Incomplete("pause_turn"), false); err != nil || got != "pause_turn" {
		t.Fatalf("pause terminal = %q, want pause_turn", got)
	}
}

func TestMessagesTerminalProjectionPrecedence(t *testing.T) {
	tests := []struct {
		name       string
		completion canonical.Completion
		hasTools   bool
		want       string
		wantErr    bool
	}{
		{name: "completed tool use", completion: canonical.Completed("completed"), hasTools: true, want: "tool_use"},
		{name: "incomplete with tool use", completion: canonical.Incomplete("length"), hasTools: true, want: "max_tokens"},
		{name: "declined with tool use", completion: canonical.Declined("safety"), hasTools: true, want: "refusal"},
		{name: "failed with tool use", completion: canonical.Failed("interrupted"), hasTools: true, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := messagesStopReasonForCompletion(test.completion, test.hasTools)
			if test.wantErr {
				if err == nil {
					t.Fatalf("stop reason = %q, want projection error", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("stop reason = %q, want %q", got, test.want)
			}
		})
	}
}
