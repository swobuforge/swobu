package chatcompletions

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func TestChatBufferedResponseNormalizesNullToolCallsToAbsent(t *testing.T) {
	providerDocument := carrier.Document{
		Family: protocolkind.ChatCompletions,
		Media:  "application/json",
		Raw:    []byte(`{"id":"chatcmpl_affine","model":"m","choices":[{"message":{"role":"assistant","content":"OK","tool_calls":null},"finish_reason":"stop"}]}`),
	}
	decoded, err := (ProviderDocumentDecoder{}).DecodeProviderDocument(context.Background(), canonical.CanonicalRequest{}, nil, providerDocument, "ex_affine_null_tool_calls")
	if err != nil {
		t.Fatal(err)
	}
	binding := canonical.ResponseBinding{SwobuID: canonical.NewSwobuResponseID("resp_affine"), TargetID: "target", TargetVersion: 1}
	response, err := canonical.ProjectStream(context.Background(), canonical.NewBoundResponseIdentityStream(decoded.Stream, binding), binding)
	if err != nil {
		t.Fatal(err)
	}
	items := response.Items()
	if len(items) != 1 {
		t.Fatalf("canonical items = %#v, want one message", items)
	}
	message, ok := items[0].Message()
	if !ok || len(message.Content()) != 1 {
		t.Fatalf("canonical output = %#v, want one text message", items[0])
	}
	text, ok := message.Content()[0].Text()
	if !ok || text.Text() != "OK" {
		t.Fatalf("canonical text = %#v, want OK", message.Content())
	}
	for _, item := range items {
		if _, ok := item.ToolCall(); ok {
			t.Fatalf("null tool_calls created canonical tool call: %#v", item)
		}
	}
	if response.Completion() != canonical.Completed("stop") {
		t.Fatalf("completion = %#v, want stop", response.Completion())
	}
	encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, *response)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Changes) != 0 || len(encoded.Changes) != 0 {
		t.Fatalf("unexpected compatibility changes: decode=%#v encode=%#v", decoded.Changes, encoded.Changes)
	}
	var envelope struct {
		Choices []struct {
			Message map[string]json.RawMessage `json:"message"`
			Finish  string                     `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(encoded.Document.RawBytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Choices) != 1 || envelope.Choices[0].Finish != "stop" {
		t.Fatalf("reconstructed completion = %s", encoded.Document.RawBytes())
	}
	if _, exists := envelope.Choices[0].Message["tool_calls"]; exists {
		t.Fatalf("reconstructed assistant message retained tool_calls: %s", encoded.Document.RawBytes())
	}
	var reconstructedText string
	if err := json.Unmarshal(envelope.Choices[0].Message["content"], &reconstructedText); err != nil || reconstructedText != "OK" {
		t.Fatalf("reconstructed assistant text = %q, error = %v", reconstructedText, err)
	}
}

func TestChatStreamEncoderEmitsOnlyStreamChoiceShape(t *testing.T) {
	inputTokens, outputTokens, reasoningTokens := 11, 7, 3
	usage, err := canonical.NewTokenUsage(canonical.TokenUsageParams{
		InputTokens:     &inputTokens,
		OutputTokens:    &outputTokens,
		ReasoningTokens: &reasoningTokens,
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name          string
		events        []sse.StreamEvent
		wantRole      bool
		wantContent   bool
		wantToolType  string
		wantToolInput string
		wantFinish    string
		wantUsage     bool
	}{
		{
			name:     "initial role",
			events:   []sse.StreamEvent{{Kind: sse.StreamEventStarted, ResultID: "chat_1", Model: "m"}},
			wantRole: true,
		},
		{
			name:        "text delta",
			events:      []sse.StreamEvent{{Kind: sse.StreamEventStarted, ResultID: "chat_1", Model: "m"}, {Kind: sse.StreamEventTextDelta, TextDelta: "ok"}},
			wantContent: true,
		},
		{
			name: "function tool start and arguments",
			events: []sse.StreamEvent{
				{Kind: sse.StreamEventStarted, ResultID: "chat_1", Model: "m"},
				{Kind: sse.StreamEventItemStarted, ItemKind: canonical.ItemKindToolCall, ItemID: "item_0", ToolUseID: "call_1", Name: "lookup", ToolType: canonical.ToolTypeFunction},
				{Kind: sse.StreamEventToolUseArgumentsDelta, ItemID: "item_0", ArgumentsDelta: `{"q":"one"}`},
			},
			wantToolType:  canonical.ToolTypeFunction,
			wantToolInput: `{"q":"one"}`,
		},
		{
			name: "custom tool start and input",
			events: []sse.StreamEvent{
				{Kind: sse.StreamEventStarted, ResultID: "chat_1", Model: "m"},
				{Kind: sse.StreamEventItemStarted, ItemKind: canonical.ItemKindToolCall, ItemID: "item_0", ToolUseID: "call_1", Name: "shell", ToolType: canonical.ToolTypeCustom},
				{Kind: sse.StreamEventToolUseArgumentsDelta, ItemID: "item_0", ArgumentsDelta: "echo hi"},
			},
			wantToolType:  canonical.ToolTypeCustom,
			wantToolInput: "echo hi",
		},
		{
			name: "terminal stop with usage",
			events: []sse.StreamEvent{
				{Kind: sse.StreamEventStarted, ResultID: "chat_1", Model: "m"},
				{Kind: sse.StreamEventTextDelta, TextDelta: "ok"},
				{Kind: sse.StreamEventCompleted, Completion: canonical.Completed("stop"), Usage: usage},
			},
			wantFinish: "stop",
			wantUsage:  true,
		},
		{
			name: "terminal tool calls",
			events: []sse.StreamEvent{
				{Kind: sse.StreamEventStarted, ResultID: "chat_1", Model: "m"},
				{Kind: sse.StreamEventItemStarted, ItemKind: canonical.ItemKindToolCall, ItemID: "item_0", ToolUseID: "call_1", Name: "lookup", ToolType: canonical.ToolTypeFunction},
				{Kind: sse.StreamEventCompleted, Completion: canonical.Completed("stop")},
			},
			wantFinish: "tool_calls",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoder := &chatCompletionsEnvelopeStreamEncoder{includeUsageFrame: test.wantUsage}
			var frames [][]byte
			for _, event := range test.events {
				emitted, err := encoder.Encode(event)
				if err != nil {
					t.Fatal(err)
				}
				frames = append(frames, emitted...)
			}
			assertChatStreamingChoiceShape(t, frames)
			wire := bytes.Join(frames, nil)
			if test.wantRole && !bytes.Contains(wire, []byte(`"role":"assistant"`)) {
				t.Fatalf("initial stream frame lacks assistant role: %s", wire)
			}
			if test.wantContent && !bytes.Contains(wire, []byte(`"content":"ok"`)) {
				t.Fatalf("text stream lacks content delta: %s", wire)
			}
			if test.wantToolType != "" && !bytes.Contains(wire, []byte(`"type":"`+test.wantToolType+`"`)) {
				t.Fatalf("tool stream lacks type %q: %s", test.wantToolType, wire)
			}
			if test.wantToolInput != "" && !chatStreamContainsToolInput(t, frames, test.wantToolInput) {
				t.Fatalf("tool stream lacks semantic input %q: %s", test.wantToolInput, wire)
			}
			if test.wantFinish != "" {
				if !bytes.Contains(wire, []byte(`"delta":{}`)) || !bytes.Contains(wire, []byte(`"finish_reason":"`+test.wantFinish+`"`)) {
					t.Fatalf("terminal stream shape is incomplete: %s", wire)
				}
				if bytes.Count(wire, []byte("data: [DONE]\n\n")) != 1 || !bytes.HasSuffix(wire, []byte("data: [DONE]\n\n")) {
					t.Fatalf("[DONE] must appear exactly once and last: %s", wire)
				}
			}
			if test.wantUsage && !bytes.Contains(wire, []byte(`"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18`)) {
				t.Fatalf("terminal stream lacks usage: %s", wire)
			}
			if test.wantFinish != "" && bytes.Contains(wire, []byte(`"finish_reason":"`+test.wantFinish+`","usage":{`)) {
				t.Fatalf("terminal choice chunk must not carry usage: %s", wire)
			}
		})
	}
}

func chatStreamContainsToolInput(t *testing.T, frames [][]byte, want string) bool {
	t.Helper()
	for _, frame := range frames {
		data := strings.TrimSpace(strings.TrimPrefix(string(frame), "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var envelope struct {
			Choices []struct {
				Delta struct {
					ToolCalls []struct {
						Function *struct {
							Arguments string `json:"arguments"`
						} `json:"function"`
						Custom *struct {
							Input string `json:"input"`
						} `json:"custom"`
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &envelope); err != nil {
			t.Fatal(err)
		}
		for _, choice := range envelope.Choices {
			for _, call := range choice.Delta.ToolCalls {
				if call.Function != nil && call.Function.Arguments == want {
					return true
				}
				if call.Custom != nil && call.Custom.Input == want {
					return true
				}
			}
		}
	}
	return false
}

func TestChatBufferedEncoderEmitsOnlyBufferedChoiceShape(t *testing.T) {
	functionKey := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	customKey := canonicaltest.MustRequestToolKey(canonical.ToolKindCustom, "shell")
	functionCall := canonicaltest.ToolCall(t, "call_function", functionKey, canonical.NewJSONObjectToolInput(canonicaltest.Object(t, `{"q":"one"}`)))
	customCall := canonicaltest.ToolCall(t, "call_custom", customKey, canonical.NewTextToolInput("echo hi"))
	message := canonicaltest.Message(t, canonical.MessageRoleAssistant, "ok")
	inputTokens, outputTokens := 4, 2
	usage, err := canonical.NewTokenUsage(canonical.TokenUsageParams{InputTokens: &inputTokens, OutputTokens: &outputTokens})
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name  string
		items []canonical.CanonicalItem
		usage canonical.TokenUsage
	}{
		{name: "text", items: []canonical.CanonicalItem{message}},
		{name: "function tool call", items: []canonical.CanonicalItem{functionCall}},
		{name: "custom tool call", items: []canonical.CanonicalItem{customCall}},
		{name: "mixed text and tool call", items: []canonical.CanonicalItem{message, functionCall}},
		{name: "usage", items: []canonical.CanonicalItem{message}, usage: usage},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := canonicaltest.ResponseWithUsage(t, "resp_1", "m", test.items, canonical.Completed("stop"), test.usage)
			encoded, err := (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
			if err != nil {
				t.Fatal(err)
			}
			var envelope struct {
				Choices []map[string]json.RawMessage `json:"choices"`
			}
			if err := json.Unmarshal(encoded.Document.RawBytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if len(envelope.Choices) != 1 {
				t.Fatalf("buffered choices = %d, want 1", len(envelope.Choices))
			}
			choice := envelope.Choices[0]
			if _, exists := choice["message"]; !exists {
				t.Fatalf("buffered choice lacks message: %s", encoded.Document.RawBytes())
			}
			if _, exists := choice["delta"]; exists {
				t.Fatalf("buffered choice contains stream delta: %s", encoded.Document.RawBytes())
			}
			var messageBody struct {
				Role string `json:"role"`
			}
			if err := json.Unmarshal(choice["message"], &messageBody); err != nil {
				t.Fatal(err)
			}
			if messageBody.Role != "assistant" {
				t.Fatalf("buffered message role = %q, want assistant", messageBody.Role)
			}
		})
	}
}

func TestChatStreamDoesNotExposeKnownUsageWithoutClientPreference(t *testing.T) {
	input, output := 10, 2
	usage, _ := canonical.NewTokenUsage(canonical.TokenUsageParams{InputTokens: &input, OutputTokens: &output})
	encoder := &chatCompletionsEnvelopeStreamEncoder{}
	var frames [][]byte
	for _, event := range []sse.StreamEvent{
		{Kind: sse.StreamEventStarted, ResultID: "chat_1", Model: "m"},
		{Kind: sse.StreamEventTextDelta, TextDelta: "ok"},
		{Kind: sse.StreamEventCompleted, Completion: canonical.Completed("stop"), Usage: usage},
	} {
		emitted, err := encoder.Encode(event)
		if err != nil {
			t.Fatal(err)
		}
		frames = append(frames, emitted...)
	}
	wire := bytes.Join(frames, nil)
	if bytes.Contains(wire, []byte(`"usage":{`)) || bytes.Contains(wire, []byte(`"choices":[]`)) {
		t.Fatalf("unrequested usage escaped into Chat stream: %s", wire)
	}
	if !bytes.Contains(wire, []byte(`"usage":null`)) {
		t.Fatalf("ordinary Chat chunks must carry usage null: %s", wire)
	}
}

func assertChatStreamingChoiceShape(t *testing.T, frames [][]byte) {
	t.Helper()
	for _, frame := range frames {
		data := strings.TrimSpace(strings.TrimPrefix(string(frame), "data:"))
		if data == "" || data == "[DONE]" || strings.Contains(data, `"error"`) {
			continue
		}
		var envelope struct {
			Choices []map[string]json.RawMessage `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &envelope); err != nil {
			t.Fatalf("invalid stream JSON: %v; data=%s", err, data)
		}
		for _, choice := range envelope.Choices {
			if _, exists := choice["message"]; exists {
				t.Fatalf("stream choice contains buffered message: %s", data)
			}
			if _, exists := choice["delta"]; !exists {
				t.Fatalf("stream choice is missing delta: %s", data)
			}
		}
	}
}
