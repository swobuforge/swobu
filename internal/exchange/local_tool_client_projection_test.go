package exchange

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestLocalToolClientProjectionKeepsLiveMessagesAcrossRounds(t *testing.T) {
	id := canonical.SwobuResponseID("swobu_projection")
	p := newLocalToolClientProjection(id)
	projector := canonical.NewResponseProjector(canonical.ResponseBinding{SwobuID: id})
	message := func(text string) canonical.CanonicalItem {
		t.Helper()
		item, err := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart(text)})
		if err != nil {
			t.Fatal(err)
		}
		return item
	}
	key, _ := canonical.NewToolKey("workspace/mcp", canonical.ToolKindFunction, "docs__search")
	callID, _ := canonical.NewToolCallID("call_selected")
	input, _ := canonical.ParseJSONObject([]byte(`{"query":"docs"}`))
	call, err := canonical.NewToolCallItem(callID, key, canonical.NewJSONObjectToolInput(input))
	if err != nil {
		t.Fatal(err)
	}
	var delivered []canonical.Event
	accept := func(event canonical.Event) {
		t.Helper()
		if event.Seq != int64(len(delivered)+1) || event.Meta.NativeID != "" || event.Meta.NativeIndex != nil {
			t.Fatalf("unstable event identity: %#v", event)
		}
		if err := projector.Apply(event); err != nil {
			t.Fatal(err)
		}
		delivered = append(delivered, event)
	}
	applyRound := func(items []canonical.CanonicalItem) canonical.CanonicalResponse {
		t.Helper()
		response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: id}, "fixture", items, canonical.Completed("stop"), canonical.NewUnknownTokenUsage())
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range canonical.SynthesizeResponseEnvelopeEvents("exchange", response.Response(), response.Model(), response.Items(), response.Completion(), response.Usage()) {
			nativeIndex := 0
			event.Meta.NativeID, event.Meta.NativeIndex = "repeated_provider_id", &nativeIndex
			out, visible, err := p.apply(event)
			if err != nil {
				t.Fatal(err)
			}
			if visible {
				if out.Kind == canonical.EventFinish || out.Kind == canonical.EventEnvelopeEnd || out.Kind == canonical.EventArgsDelta {
					t.Fatalf("intermediate tool/terminal escaped: %#v", out)
				}
				accept(out)
			}
		}
		return response
	}
	applyRound([]canonical.CanonicalItem{message("checking docs"), call})
	if len(delivered) != 6 || delivered[4].Kind != canonical.EventTextDelta {
		t.Fatalf("message prefix was not exposed before round decision: %#v", delivered)
	}
	p.beginRound()
	final := applyRound([]canonical.CanonicalItem{message("finished")})
	terminal, err := p.finish("exchange", final, final.Usage())
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range terminal {
		accept(event)
	}
	response, err := projector.Done()
	if err != nil {
		t.Fatal(err)
	}
	items := response.Items()
	if len(items) != 2 || response.Response().SwobuID != id {
		t.Fatalf("aggregate response lost its messages/identity: %#v", response)
	}
	for index, want := range []string{"checking docs", "finished"} {
		got, _ := items[index].Message()
		text, _ := got.Content()[0].Text()
		if text.Text() != want {
			t.Fatalf("message %d = %q", index, text.Text())
		}
	}
	if _, err := p.finish("exchange", final, final.Usage()); err == nil {
		t.Fatal("duplicate completion accepted")
	}
}

func TestLocalToolClientProjectionPreservesCallerToolMessageOrder(t *testing.T) {
	id := canonical.SwobuResponseID("swobu_final_tool")
	p := newLocalToolClientProjection(id)
	p.ownsTool = func(canonical.ToolKey) bool { return false }
	key, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "client_tool")
	callID, _ := canonical.NewToolCallID("call_client")
	input, _ := canonical.ParseJSONObject([]byte(`{"x":1}`))
	call, err := canonical.NewToolCallItem(callID, key, canonical.NewJSONObjectToolInput(input))
	if err != nil {
		t.Fatal(err)
	}
	message, _ := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart("your turn")})
	tokens := 7
	usage, _ := canonical.NewTokenUsage(canonical.TokenUsageParams{OutputTokens: &tokens})
	response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: id}, "fixture", []canonical.CanonicalItem{call, message}, canonical.Completed("tool_calls"), usage)
	if err != nil {
		t.Fatal(err)
	}
	projector := canonical.NewResponseProjector(canonical.ResponseBinding{SwobuID: id})
	for _, event := range canonical.SynthesizeResponseEnvelopeEvents("exchange", response.Response(), response.Model(), response.Items(), response.Completion(), response.Usage()) {
		out, visible, err := p.apply(event)
		if err != nil {
			t.Fatal(err)
		}
		if visible {
			if err := projector.Apply(out); err != nil {
				t.Fatal(err)
			}
		}
	}
	total := canonical.SumTokenUsage(usage, usage)
	terminal, err := p.finish("exchange", response, total)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range terminal {
		if err := projector.Apply(event); err != nil {
			t.Fatal(err)
		}
	}
	final, err := projector.Done()
	if err != nil {
		t.Fatal(err)
	}
	items := final.Items()
	if len(items) != 2 || items[0].Kind() != canonical.ItemKindToolCall || items[1].Kind() != canonical.ItemKindMessage {
		t.Fatalf("caller tool/message order changed: %#v", items)
	}
	gotCall, _ := items[0].ToolCall()
	if gotCall.CallID() != callID || gotCall.Tool() != key {
		t.Fatal("final caller tool lost its causal/declaration identity")
	}
	if got, known := final.Usage().OutputTokens(); !known || got != 14 {
		t.Fatalf("all-round usage = %d, %t", got, known)
	}
}
