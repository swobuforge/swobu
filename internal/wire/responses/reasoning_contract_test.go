package responses

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

func TestResponsesReasoningContextDecodesAndEncodesExactly(t *testing.T) {
	for _, value := range []canonical.ResponsesReasoningContext{
		canonical.ResponsesReasoningContextAuto,
		canonical.ResponsesReasoningContextAllTurns,
		canonical.ResponsesReasoningContextCurrentTurn,
	} {
		t.Run(string(value), func(t *testing.T) {
			raw := []byte(`{"model":"gpt","input":"hi","reasoning":{"context":"` + string(value) + `"}}`)
			decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}))
			if err != nil {
				t.Fatal(err)
			}
			got, present := decoded.Request.Request.Reasoning().ResponsesContextField().Get()
			if !present || got != value {
				t.Fatalf("decoded context = (%q,%t), want (%q,true)", got, present, value)
			}
			document, err := EncodeCarrierWithChanges(EncodeInput{Request: decoded.Request.Request, ToolNames: testAttemptToolNames(decoded.Request.Request)}, delivery.BufferedDelivery(), nil, "", EncodeOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(document.RawBytes()), `"context":"`+string(value)+`"`) {
				t.Fatalf("encoded = %s", document.RawBytes())
			}
			result, err := (ProviderRequestDocumentEncoder{}).EncodeProviderRequestDocument(
				wire.ProviderEncodeInput{Request: decoded.Request.Request, ToolNames: testAttemptToolNames(decoded.Request.Request)},
				delivery.BufferedDelivery(),
				"exchange",
			)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Changes) != 0 {
				t.Fatalf("exact reasoning context changes = %#v", result.Changes)
			}
		})
	}
}

func TestResponsesReasoningContextRejectsMalformedValuesAsBadRequest(t *testing.T) {
	for _, rawContext := range []string{`"future"`, `""`, `17`, `{}`} {
		raw := []byte(`{"model":"gpt","input":"hi","reasoning":{"context":` + rawContext + `}}`)
		_, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}))
		var canonicalErr canonical.Error
		if !errors.As(err, &canonicalErr) || canonicalErr.Code != canonical.ErrorCodeBadRequest {
			t.Fatalf("context %s error = %#v, want typed bad request", rawContext, err)
		}
	}
}

func TestResponsesReasoningContextSurvivesImplicitRebase(t *testing.T) {
	raw := []byte(`{
		"model":"gpt",
		"reasoning":{"context":"current_turn"},
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"first"}]},
			{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"answer"}]},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"next"}]}
		]
	}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Request.RebasedRequest == nil {
		t.Fatal("expected implicit Responses rebase")
	}
	for name, request := range map[string]canonical.CanonicalRequest{
		"full":    decoded.Request.Request,
		"rebased": decoded.Request.RebasedRequest.Request,
	} {
		contextValue, present := request.Reasoning().ResponsesContextField().Get()
		if !present || contextValue != canonical.ResponsesReasoningContextCurrentTurn {
			t.Fatalf("%s context = (%q,%t)", name, contextValue, present)
		}
	}
}

func TestResponsesReasoningRequestNormalizesToMinimalControls(t *testing.T) {
	raw := []byte(`{"model":"gpt","input":"hi","reasoning":{"effort":"high","summary":"detailed"}}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}
	request := decoded.Request.Request
	compute, ok := request.Reasoning().ComputeField().Get()
	if !ok || compute.Kind() != canonical.ReasoningAutomatic {
		t.Fatalf("compute = %#v", compute)
	}
	document, err := EncodeCarrierWithChanges(EncodeInput{Request: request, ToolNames: testAttemptToolNames(request)}, delivery.BufferedDelivery(), nil, "", EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document.RawBytes()), `"summary":"auto"`) || !strings.Contains(string(document.RawBytes()), `"effort":"high"`) || !strings.Contains(string(document.RawBytes()), `"reasoning.encrypted_content"`) {
		t.Fatalf("encoded = %s", document.RawBytes())
	}
}

func TestResponsesFutureReasoningQualityHintsApproximateToOmission(t *testing.T) {
	raw := []byte(`{"model":"gpt","input":"hi","reasoning":{"effort":"future_effort","summary":"future_summary"}}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Request.Request.Reasoning().ComputeField().IsSpecified() ||
		decoded.Request.Request.Reasoning().DisclosureField().IsSpecified() {
		t.Fatalf("future reasoning hints survived canonically: %#v", decoded.Request.Request.Reasoning())
	}
	if len(decoded.Changes) != 1 {
		t.Fatalf("changes = %#v, want one deduplicated approximation", decoded.Changes)
	}
	for _, decision := range decoded.Changes {
		if decision.Capability != canonical.RequestReasoning || decision.Kind != compat.Approximation {
			t.Fatalf("decision = %#v, want request.reasoning Approx", decision)
		}
	}
}

func TestResponsesPreservesClientEncryptedContinuation(t *testing.T) {
	raw := []byte(`{"model":"gpt","input":[{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"brief"}],"encrypted_content":"secret"}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}
	items := decoded.Request.Request.Items()
	reasoning, ok := items[0].Reasoning()
	if len(items) != 1 || !ok {
		t.Fatalf("canonical reasoning was not preserved: %#v", items)
	}
	replay, ok := reasoning.Opaque().Responses()
	if !ok || replay.EncryptedContent != "secret" {
		t.Fatalf("encrypted reasoning replay = %#v", replay)
	}
	// RFC G2 §7.3: the paired Responses wire id survives client ingress verbatim,
	// so a later stateless turn can replay the reasoning against the same item.
	if replay.ItemID != "rs_1" {
		t.Fatalf("encrypted reasoning id = %q, want rs_1", replay.ItemID)
	}
}

// RFC G2 §7.2/§7.3: a reasoning item carrying a presentation id but no
// encrypted content has no opaque continuation consumer, so the id is discarded
// and the item stays readable-only metadata. Generic support must not reject an
// idless reasoning item merely because some provider might.
func TestResponsesReasoningIDWithoutEncryptedContentIsDiscarded(t *testing.T) {
	raw := []byte(`{"model":"gpt","input":[{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"brief"}]}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.NewDocument("", "application/json", nil, raw, carrier.Meta{}))
	if err != nil {
		t.Fatal(err)
	}
	reasoning, ok := decoded.Request.Request.Items()[0].Reasoning()
	if !ok {
		t.Fatal("readable reasoning was not preserved")
	}
	if replay, ok := reasoning.Opaque().Responses(); ok {
		t.Fatalf("idless reasoning gained an opaque replay: %+v", replay)
	}
}

// RFC G2 §7.3: the buffered decode is shared by client ingress and buffered
// provider-output projection, so a provider response's reasoning item carries
// its id into the canonical replay too.
func TestResponsesProviderOutputPreservesReasoningID(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("gpt")})
	raw := []byte(`{"id":"resp","model":"gpt","status":"completed","output":[{"type":"reasoning","id":"rs_9","status":"completed","encrypted_content":"cipher","summary":[{"type":"summary_text","text":"brief"}]}]}`)
	stream, err := decodeResponseBuffered(context.Background(), request, testAttemptToolNames(request), raw, "ex", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	replay, ok := firstReasoningReplay(t, stream)
	if !ok {
		t.Fatal("buffered provider reasoning item was not emitted")
	}
	if replay.EncryptedContent != "cipher" || replay.ItemID != "rs_9" {
		t.Fatalf("provider reasoning replay = %+v, want cipher/rs_9", replay)
	}
}

// RFC G2 §7.4: the committed stream identity (merged across frames) is the
// replay id, not whichever terminal frame happens to carry one. The id survives
// whether it arrives only on output_item.added, only on the terminal item, or
// repeated on both.
func TestResponsesStreamReasoningIDSurvivesFramePositions(t *testing.T) {
	for _, name := range []string{"id-on-added", "id-on-done", "id-on-both"} {
		t.Run(name, func(t *testing.T) {
			added := `{"id":"rs_42","type":"reasoning","status":"in_progress"}`
			done := `{"id":"rs_42","type":"reasoning","status":"completed","encrypted_content":"cipher","summary":[{"type":"summary_text","text":"brief"}]}`
			switch name {
			case "id-on-done":
				added = `{"type":"reasoning","status":"in_progress"}`
			case "id-on-added":
				done = `{"type":"reasoning","status":"completed","encrypted_content":"cipher","summary":[{"type":"summary_text","text":"brief"}]}`
			}
			raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp\",\"model\":\"gpt\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
				"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":" + added + "}\n\n" +
				"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":" + done + "}\n\n" +
				"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp\",\"model\":\"gpt\",\"status\":\"completed\",\"output\":[]}}\n\n"
			request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("gpt")})
			stream := decodeResponseStream(request, testAttemptToolNames(request), carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil, true)
			replay, ok := firstReasoningReplay(t, stream)
			if !ok {
				t.Fatal("streamed reasoning item was not emitted")
			}
			if replay.EncryptedContent != "cipher" || replay.ItemID != "rs_42" {
				t.Fatalf("streamed reasoning replay = %+v, want cipher/rs_42", replay)
			}
		})
	}
}

// Buffered terminal output and streamed item-done output share portable
// completed reasoning semantics, while each preserves the usable opaque replay
// selected by its own authority boundary.
func TestResponsesStreamAndBufferedReasoningConvergeWithoutOpaqueReplayEquality(t *testing.T) {
	bufferedRaw := []byte(`{"id":"resp","model":"gpt","status":"completed","output":[{"type":"reasoning","id":"rs_7","status":"completed","encrypted_content":"cipher-B","summary":[{"type":"summary_text","text":"brief"}]}]}`)
	streamRaw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp\",\"model\":\"gpt\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"rs_7\",\"type\":\"reasoning\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"rs_7\",\"type\":\"reasoning\",\"status\":\"completed\",\"encrypted_content\":\"cipher-A\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"brief\"}]}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp\",\"model\":\"gpt\",\"status\":\"completed\",\"output\":[{\"id\":\"rs_7\",\"type\":\"reasoning\",\"status\":\"completed\",\"encrypted_content\":\"cipher-B\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"brief\"}]}]}}\n\n"
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("gpt")})
	buffered, err := decodeResponseBuffered(context.Background(), request, testAttemptToolNames(request), bufferedRaw, "ex", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	bufferedReasoning, bufferedReplay := firstReasoningItem(t, buffered)
	streamedReasoning, streamedReplay := firstReasoningItem(t, decodeResponseStream(request, testAttemptToolNames(request), carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(streamRaw))}, "ex", nil, true))
	if len(bufferedReasoning.Parts()) != 1 || len(streamedReasoning.Parts()) != 1 || bufferedReasoning.Parts()[0] != streamedReasoning.Parts()[0] {
		t.Fatalf("portable reasoning differs: buffered=%#v streamed=%#v", bufferedReasoning.Parts(), streamedReasoning.Parts())
	}
	if bufferedReplay.ItemID != "rs_7" || bufferedReplay.EncryptedContent != "cipher-B" {
		t.Fatalf("buffered replay = %+v", bufferedReplay)
	}
	if streamedReplay.ItemID != "rs_7" || streamedReplay.EncryptedContent != "cipher-A" {
		t.Fatalf("streamed replay = %+v", streamedReplay)
	}
}

func TestResponsesStreamAcceptsTerminalReasoningWithOmittedCommittedID(t *testing.T) {
	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp\",\"model\":\"gpt\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"rs_7\",\"type\":\"reasoning\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"rs_7\",\"type\":\"reasoning\",\"status\":\"completed\",\"encrypted_content\":\"cipher\",\"summary\":[]}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp\",\"model\":\"gpt\",\"status\":\"completed\",\"output\":[{\"type\":\"reasoning\",\"status\":\"completed\",\"encrypted_content\":\"cipher\",\"summary\":[]}]}}\n\n"
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("gpt")})
	stream := decodeResponseStream(request, testAttemptToolNames(request), carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil, true)
	replay, ok := firstReasoningReplay(t, stream)
	if !ok || replay.EncryptedContent != "cipher" || replay.ItemID != "rs_7" {
		t.Fatalf("streamed reasoning replay = %+v, present=%v", replay, ok)
	}
	for {
		_, err := stream.Next(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("terminal identity observation failed: %v", err)
		}
	}
}

func TestResponsesStreamCommitsDeferredReasoningWithOmittedTerminalID(t *testing.T) {
	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp\",\"model\":\"gpt\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"rs_7\",\"type\":\"reasoning\",\"status\":\"in_progress\"}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"rs_7\",\"type\":\"reasoning\",\"status\":\"incomplete\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"partial\"}]}}\n\n" +
		"event: response.incomplete\ndata: {\"type\":\"response.incomplete\",\"response\":{\"id\":\"resp\",\"model\":\"gpt\",\"status\":\"incomplete\",\"output\":[{\"type\":\"reasoning\",\"status\":\"incomplete\",\"summary\":[{\"type\":\"summary_text\",\"text\":\"partial\"}]}]}}\n\n"
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("gpt")})
	response := readResponsesStreamResponse(t, request, raw)
	if response.Completion().Reason() != "incomplete" || len(response.Items()) != 1 {
		t.Fatalf("deferred reasoning completion=%q items=%#v", response.Completion().Reason(), response.Items())
	}
	reasoning, ok := response.Items()[0].Reasoning()
	if !ok || len(reasoning.Parts()) != 1 || reasoning.Parts()[0].Text() != "partial" {
		t.Fatalf("deferred reasoning = %#v", response.Items()[0])
	}
}

func firstReasoningReplay(t *testing.T, stream canonical.ResponseStream) (canonical.ResponsesReasoningReplay, bool) {
	t.Helper()
	for {
		event, err := stream.Next(context.Background())
		if err != nil {
			return canonical.ResponsesReasoningReplay{}, false
		}
		if event.Kind != canonical.EventItemCompleted {
			continue
		}
		item := event.Payload.(canonical.ItemEvent).Payload.(canonical.ItemCompletedPayload).Item
		if reasoning, ok := item.Reasoning(); ok {
			if replay, ok := reasoning.Opaque().Responses(); ok {
				return replay, true
			}
		}
	}
}

func firstReasoningItem(t *testing.T, stream canonical.ResponseStream) (canonical.ReasoningItem, canonical.ResponsesReasoningReplay) {
	t.Helper()
	for {
		event, err := stream.Next(context.Background())
		if err != nil {
			t.Fatalf("reasoning item missing: %v", err)
		}
		if event.Kind != canonical.EventItemCompleted {
			continue
		}
		item := event.Payload.(canonical.ItemEvent).Payload.(canonical.ItemCompletedPayload).Item
		reasoning, ok := item.Reasoning()
		if !ok {
			continue
		}
		replay, ok := reasoning.Opaque().Responses()
		if !ok {
			t.Fatal("reasoning item has no Responses replay")
		}
		return reasoning, replay
	}
}

func TestResponsesReasoningSummaryIsAtomic(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("gpt")})
	raw := []byte(`{"id":"resp","model":"gpt","status":"completed","output":[{"type":"reasoning","id":"rs_1","status":"completed","summary":[{"type":"summary_text","text":"brief"}]}]}`)
	stream, err := decodeResponseBuffered(context.Background(), request, testAttemptToolNames(request), raw, "ex", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for {
		event, err := stream.Next(context.Background())
		if err != nil {
			break
		}
		if event.Kind == canonical.EventItemStart || event.Kind == canonical.EventContentStart || event.Kind == canonical.EventTextDelta {
			if payload, ok := event.Payload.(canonical.ItemEvent); ok && payload.Position.Item == 0 {
				t.Fatalf("reasoning emitted progressive event %q", event.Kind)
			}
		}
		if event.Kind == canonical.EventItemCompleted {
			item := event.Payload.(canonical.ItemEvent).Payload.(canonical.ItemCompletedPayload).Item
			found = found || item.Kind() == canonical.ItemKindReasoning
		}
	}
	if !found {
		t.Fatal("reasoning summary completion was not emitted")
	}
}

func TestResponsesEmptyReasoningArtifactsAreIgnored(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("gpt")})
	buffered, err := decodeResponseBuffered(context.Background(), request, testAttemptToolNames(request), []byte(`{"id":"resp","model":"gpt","status":"completed","output":[{"type":"reasoning","id":"rs_1","status":"completed","summary":[]}]}`), "ex", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	assertNoReasoningItem(t, buffered)

	raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp\",\"model\":\"gpt\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"rs_1\",\"type\":\"reasoning\",\"status\":\"in_progress\",\"summary\":[]}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"rs_1\",\"type\":\"reasoning\",\"status\":\"completed\",\"summary\":[]}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp\",\"model\":\"gpt\",\"status\":\"completed\",\"output\":[]}}\n\n"
	assertNoReasoningItem(t, decodeResponseStream(request, testAttemptToolNames(request), carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil, true))
}

func assertNoReasoningItem(t *testing.T, stream canonical.ResponseStream) {
	t.Helper()
	for {
		event, err := stream.Next(context.Background())
		if err != nil {
			return
		}
		if event.Kind == canonical.EventItemCompleted {
			item := event.Payload.(canonical.ItemEvent).Payload.(canonical.ItemCompletedPayload).Item
			if item.Kind() == canonical.ItemKindReasoning {
				t.Fatal("empty reasoning artifact produced a canonical item")
			}
		}
	}
}

func TestResponsesContinuationUsesProviderHandle(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gpt"),
	})
	document, err := EncodeCarrierWithChanges(EncodeInput{Request: request, PreviousHistory: responsesPrevious("provider_resp"), ToolNames: testAttemptToolNames(request)}, delivery.BufferedDelivery(), nil, "", EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document.RawBytes()), `"previous_response_id":"provider_resp"`) {
		t.Fatalf("encoded = %s", document.RawBytes())
	}
}

func TestResponsesWireStreamAndDocumentPreserveProviderReasoningItemID(t *testing.T) {
	opaque, err := canonical.NewResponsesOpaqueThinking(canonical.ResponsesReasoningReplay{
		EncryptedContent: "ey_test_encrypted_token",
		ItemID:           "rs_provider_assigned_123",
	})
	if err != nil {
		t.Fatal(err)
	}
	part, err := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "test reasoning summary")
	if err != nil {
		t.Fatal(err)
	}
	reasoningItem, err := canonical.NewReasoningItem([]canonical.ReasoningPart{part}, opaque)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Test streaming wire encoder preserves provider ItemID across all reasoning frames
	wireEncoder := NewResponseStreamWireEncoder()
	_, err = wireEncoder.Encode(sse.StreamEvent{
		Kind:     sse.StreamEventStarted,
		ResultID: "resp_test",
		Model:    "gpt-4o",
	})
	if err != nil {
		t.Fatal(err)
	}
	frames, err := wireEncoder.Encode(sse.StreamEvent{
		Kind:          sse.StreamEventItemCompleted,
		ItemKind:      canonical.ItemKindReasoning,
		ItemOrdinal:   0,
		CompletedItem: &reasoningItem,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 3 {
		t.Fatalf("expected 3 reasoning frames (added, summary_part, done), got %d: %#v", len(frames), frames)
	}

	var addedFrame, summaryFrame, doneFrame map[string]any
	if err := json.Unmarshal(frames[0], &addedFrame); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(frames[1], &summaryFrame); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(frames[2], &doneFrame); err != nil {
		t.Fatal(err)
	}

	// Verify output_item.added has provider ID
	addedItem := addedFrame["item"].(map[string]any)
	if addedItem["id"] != "rs_provider_assigned_123" {
		t.Fatalf("output_item.added item.id = %v, want rs_provider_assigned_123", addedItem["id"])
	}

	// Verify reasoning_summary_part.added has provider item_id
	if summaryFrame["item_id"] != "rs_provider_assigned_123" {
		t.Fatalf("reasoning_summary_part.added item_id = %v, want rs_provider_assigned_123", summaryFrame["item_id"])
	}

	// Verify output_item.done has provider ID and encrypted_content
	doneItem := doneFrame["item"].(map[string]any)
	if doneItem["id"] != "rs_provider_assigned_123" {
		t.Fatalf("output_item.done item.id = %v, want rs_provider_assigned_123", doneItem["id"])
	}
	if doneItem["encrypted_content"] != "ey_test_encrypted_token" {
		t.Fatalf("output_item.done encrypted_content = %v, want ey_test_encrypted_token", doneItem["encrypted_content"])
	}

	// Verify response.completed output items contain provider ID
	completedFrames, err := wireEncoder.Encode(sse.StreamEvent{
		Kind:       sse.StreamEventCompleted,
		Completion: canonical.Completed("completed"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var completedFrame map[string]any
	if err := json.Unmarshal(completedFrames[0], &completedFrame); err != nil {
		t.Fatal(err)
	}
	respObj := completedFrame["response"].(map[string]any)
	outputList := respObj["output"].([]any)
	if len(outputList) == 0 {
		t.Fatal("response.completed output list is empty")
	}
	completedOutputItem := outputList[0].(map[string]any)
	if completedOutputItem["id"] != "rs_provider_assigned_123" {
		t.Fatalf("response.completed output[0].id = %v, want rs_provider_assigned_123", completedOutputItem["id"])
	}

	// 2. Test buffered response document encoder preserves provider ItemID
	canonicalResp, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: "resp_test"}, "gpt-4o", []canonical.CanonicalItem{reasoningItem}, canonical.Completed("completed"), canonical.TokenUsage{})
	if err != nil {
		t.Fatal(err)
	}
	docEncoder := ResponseDocumentEncoder{}
	encodedDoc, err := docEncoder.EncodeResponseDocument(canonical.CanonicalRequest{}, canonicalResp)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encodedDoc.Document.RawBytes(), []byte(`"id":"rs_provider_assigned_123"`)) {
		t.Fatalf("buffered response doc missing provider reasoning item id: %s", encodedDoc.Document.RawBytes())
	}
	if bytes.Contains(encodedDoc.Document.RawBytes(), []byte("rs_swobu")) {
		t.Fatalf("buffered response doc unexpectedly contains synthetic rs_swobu id: %s", encodedDoc.Document.RawBytes())
	}

	// 3. Test wire codec atomicity: exact round-tripping of wire-level structures
	clientHistoryJSON := fmt.Sprintf(`{
		"model": "gpt-4o",
		"input": [
			{"type": "reasoning", "id": "rs_provider_assigned_123", "status": "completed", "encrypted_content": "ey_test_encrypted_token", "summary": [{"type": "summary_text", "text": "test reasoning summary"}]},
			{"type": "message", "role": "user", "content": "next turn question"}
		]
	}`)
	decodedReq, err := (ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument("", "application/json", nil, []byte(clientHistoryJSON), carrier.Meta{}),
	)
	if err != nil {
		t.Fatal(err)
	}

	var changes []compat.Change
	outboundDoc, err := EncodeCarrierWithChanges(
		EncodeInput{Request: decodedReq.Request.Request, ToolNames: testAttemptToolNames(decodedReq.Request.Request)},
		delivery.BufferedDelivery(), &changes, "ex", EncodeOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(outboundDoc.RawBytes(), []byte(`"id":"rs_provider_assigned_123"`)) {
		t.Fatalf("outbound request lost original provider item id: %s", outboundDoc.RawBytes())
	}
	if !bytes.Contains(outboundDoc.RawBytes(), []byte(`"encrypted_content":"ey_test_encrypted_token"`)) {
		t.Fatalf("outbound request lost encrypted content: %s", outboundDoc.RawBytes())
	}
	if bytes.Contains(outboundDoc.RawBytes(), []byte("rs_swobu")) {
		t.Fatalf("outbound request unexpectedly contains rs_swobu: %s", outboundDoc.RawBytes())
	}
}

func TestResponsesSyntheticReasoningIDWhenReplayIDEmpty(t *testing.T) {
	part, err := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "summary only")
	if err != nil {
		t.Fatal(err)
	}
	reasoningItem, err := canonical.NewReasoningItem([]canonical.ReasoningPart{part}, canonical.OpaqueThinking{})
	if err != nil {
		t.Fatal(err)
	}

	wireEncoder := NewResponseStreamWireEncoder()
	_, err = wireEncoder.Encode(sse.StreamEvent{
		Kind:     sse.StreamEventStarted,
		ResultID: "resp_test",
		Model:    "gpt-4o",
	})
	if err != nil {
		t.Fatal(err)
	}
	frames, err := wireEncoder.Encode(sse.StreamEvent{
		Kind:          sse.StreamEventItemCompleted,
		ItemKind:      canonical.ItemKindReasoning,
		ItemOrdinal:   0,
		CompletedItem: &reasoningItem,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 3 {
		t.Fatalf("expected 3 reasoning frames, got %d", len(frames))
	}
	var addedFrame map[string]any
	if err := json.Unmarshal(frames[0], &addedFrame); err != nil {
		t.Fatal(err)
	}
	addedItem := addedFrame["item"].(map[string]any)
	if addedItem["id"] != "rs_swobu_0" {
		t.Fatalf("output_item.added item.id = %v, want synthetic rs_swobu_0", addedItem["id"])
	}
}

func TestResponsesIdlessOpaqueReplayRemainsIdless(t *testing.T) {
	// Idless opaque replay (non-empty EncryptedContent, empty ItemID) must NOT receive a synthetic presentation ID.
	opaque, err := canonical.NewResponsesOpaqueThinking(canonical.ResponsesReasoningReplay{
		EncryptedContent: "ey_idless_cipher_token",
		ItemID:           "",
	})
	if err != nil {
		t.Fatal(err)
	}
	part, err := canonical.NewReasoningPart(canonical.ReasoningPartSummary, "idless thought")
	if err != nil {
		t.Fatal(err)
	}
	reasoningItem, err := canonical.NewReasoningItem([]canonical.ReasoningPart{part}, opaque)
	if err != nil {
		t.Fatal(err)
	}

	req := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gpt-4o"),
		Items: []canonical.CanonicalItem{reasoningItem},
	})

	var changes []compat.Change
	outboundDoc, err := EncodeCarrierWithChanges(
		EncodeInput{Request: req, ToolNames: testAttemptToolNames(req)},
		delivery.BufferedDelivery(), &changes, "ex", EncodeOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}

	raw := outboundDoc.RawBytes()
	if !bytes.Contains(raw, []byte(`"encrypted_content":"ey_idless_cipher_token"`)) {
		t.Fatalf("outbound doc missing encrypted_content: %s", raw)
	}
	if bytes.Contains(raw, []byte("rs_swobu")) {
		t.Fatalf("outbound doc unexpectedly contains synthetic rs_swobu ID for idless replay: %s", raw)
	}

	var parsed struct {
		Input []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Input) != 1 {
		t.Fatalf("expected 1 input item, got %d", len(parsed.Input))
	}
	item := parsed.Input[0]
	if id, exists := item["id"]; exists && id != "" {
		t.Fatalf("idless reasoning item has unexpected non-empty id: %v", id)
	}
}
