package kimi

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestKimiCodecReplaysOpaqueThinkingAndQuantizesEffort(t *testing.T) {
	opaque, err := canonical.NewProviderChatOpaqueThinking(ChatReplayScope, []byte("private reasoning"))
	if err != nil {
		t.Fatal(err)
	}
	part, err := canonical.NewReasoningPart(canonical.ReasoningPartTrace, "private reasoning")
	if err != nil {
		t.Fatal(err)
	}
	reasoning, err := canonical.NewReasoningItem([]canonical.ReasoningPart{part}, opaque)
	if err != nil {
		t.Fatal(err)
	}
	effort := canonical.InferenceEffortXHigh
	controls, err := canonical.NewGenerationControls(canonical.GenerationControlsParams{Effort: &effort})
	if err != nil {
		t.Fatal(err)
	}
	automatic := canonical.NewAutomaticReasoningCompute()
	reasoningControls, err := canonical.NewReasoningControls(canonical.ReasoningControlsParams{Compute: canonical.Specify(automatic)})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("kimi-k3"), Items: []canonical.CanonicalItem{message(t, canonical.MessageRoleUser, "question"), reasoning, message(t, canonical.MessageRoleAssistant, "answer")}, Controls: controls, Reasoning: reasoningControls,
	})
	document, _, err := (reasoningCodec{}).Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		ReasoningEffort string           `json:"reasoning_effort"`
		Messages        []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(document.RawBytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ReasoningEffort != "max" {
		t.Fatalf("reasoning_effort = %q, want max; document=%s", payload.ReasoningEffort, document.RawBytes())
	}
	if payload.Messages[1]["reasoning_content"] != "private reasoning" {
		t.Fatalf("messages = %#v, want Kimi reasoning replay", payload.Messages)
	}
}

func TestKimiBufferedReasoningIsRemovedBeforeSharedDecode(t *testing.T) {
	document := carrier.NewDocument("", "application/json", nil, []byte(`{"choices":[{"message":{"role":"assistant","reasoning_content":"think first","content":"answer"}}]}`), carrier.Meta{})
	cleaned, item, err := protocolcodec.ExtractChatReasoningDocument(document, kimiChatReasoningExtractor{})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cleaned.RawBytes(), []byte("reasoning_content")) {
		t.Fatalf("Kimi field leaked to shared decoder: %s", cleaned.RawBytes())
	}
	reasoning, ok := item.Reasoning()
	if !ok {
		t.Fatal("reasoning item missing")
	}
	raw, ok := reasoning.Opaque().ProviderChat(ChatReplayScope)
	if !ok || string(raw) != "think first" {
		t.Fatalf("opaque = %q, %v", raw, ok)
	}
}

func TestKimiStreamingReasoningIsRemovedAndCaptured(t *testing.T) {
	raw := "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"think \"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{\"reasoning_content\":\"now\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"answer\"},\"finish_reason\":null}]}\n\ndata: [DONE]\n\n"
	body := protocolcodec.NewChatReasoningSSEBody(io.NopCloser(strings.NewReader(raw)), kimiChatReasoningExtractor{})
	cleaned, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cleaned, []byte("reasoning_content")) {
		t.Fatalf("Kimi field leaked to shared decoder: %s", cleaned)
	}
	item, ok := body.Take()
	if !ok {
		t.Fatal("streamed reasoning item missing")
	}
	reasoning, _ := item.Reasoning()
	opaque, _ := reasoning.Opaque().ProviderChat(ChatReplayScope)
	if string(opaque) != "think now" {
		t.Fatalf("opaque = %q, want exact fragments", opaque)
	}
}

func TestKimiDoesNotSerializeForeignProviderChatOpaqueThinking(t *testing.T) {
	opaque, err := canonical.NewProviderChatOpaqueThinking("foreign-chat-replay", []byte(`[{"type":"reasoning.summary","summary":"foreign"}]`))
	if err != nil {
		t.Fatal(err)
	}
	part, _ := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "foreign")
	reasoning, err := canonical.NewReasoningItem([]canonical.ReasoningPart{part}, opaque)
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("kimi-k3"), Items: []canonical.CanonicalItem{reasoning, message(t, canonical.MessageRoleAssistant, "answer")}})
	document, _, err := (reasoningCodec{}).Encode(provider.Request{Canonical: request, Delivery: delivery.BufferedDelivery()})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(document.RawBytes(), []byte("reasoning_content")) {
		t.Fatalf("foreign opaque state leaked to Kimi: %s", document.RawBytes())
	}
}

func message(t *testing.T, role canonical.MessageRole, text string) canonical.CanonicalItem {
	t.Helper()
	return canonicaltest.Message(t, role, text)
}

func TestKimiRuntimeOnlyAcceptsChatCompletions(t *testing.T) {
	_ = protocolkind.ChatCompletions
}
