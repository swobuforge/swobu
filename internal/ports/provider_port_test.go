package ports

import (
	"testing"

	"github.com/metrofun/swobu/internal/domain/compatibility"
)

func TestNewExecuteRequest_ClonesCanonicalRequestAndTargetInputs(t *testing.T) {
	request := compatibility.NewDialogRequest(
		"m",
		[]compatibility.CanonicalItem{
			compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
			compatibility.NewToolUseItem(compatibility.ItemAuthorUser, "", "toolu_1", "calculator", map[string]any{"expr": "2+2"}),
		},
	)
	target := NewRoutableTarget(
		"backend-a",
		"custom",
		"http://localhost:8080/v1",
		"cred-1",
		"chat_completions",
		"",
		"",
	)
	req := NewExecuteRequest(request, NewExecutionContract(compatibility.DeliveryModeStreaming), target)

	requestItems := request.Items()
	requestItems[0].Text = "changed"
	requestItems[1].Input["expr"] = "changed"

	if got := req.Contract.DeliveryMode; got != compatibility.DeliveryModeStreaming {
		t.Fatalf("delivery mode = %q, want %q", got, compatibility.DeliveryModeStreaming)
	}
	if got := req.Request.SemanticKind(); got != compatibility.SemanticKindConversation {
		t.Fatalf("semantic kind = %q, want %q", got, compatibility.SemanticKindConversation)
	}
	typed, ok := req.Request.(compatibility.DialogCanonicalRequest)
	if !ok {
		t.Fatalf("expected compatibility.DialogCanonicalRequest, got %T", req.Request)
	}
	if got := typed.Items()[0].Text; got != "hi" {
		t.Fatalf("message text = %q, want %q", got, "hi")
	}
	if got := typed.Items()[1].Input["expr"]; got != "2+2" {
		t.Fatalf("tool input = %v, want %q", got, "2+2")
	}
	if got := req.Target.ProtocolKind; got != "chat_completions" {
		t.Fatalf("protocol kind = %q, want %q", got, "chat_completions")
	}
}

func TestNewBufferedExecuteResponse_ClonesCanonicalOutputAndPreservesDeliveryMode(t *testing.T) {
	output := compatibility.NewConversationOutput(
		"resp_1",
		"m",
		[]compatibility.OutputItem{
			compatibility.NewTextOutputItem("text_0", "hi"),
		},
		"stop",
	)

	resp := NewBufferedExecuteResponse(output)
	items := output.Items()
	items[0].Text = "changed"

	if got := resp.DeliveryMode(); got != compatibility.DeliveryModeBuffered {
		t.Fatalf("delivery mode = %q, want %q", got, compatibility.DeliveryModeBuffered)
	}
	typed, ok := resp.Output().(compatibility.CanonicalOutputValue)
	if !ok {
		t.Fatalf("output type = %T, want compatibility.CanonicalOutputValue", resp.Output())
	}
	if got := typed.Items()[0].Text; got != "hi" {
		t.Fatalf("output text = %q, want %q", got, "hi")
	}
}

func TestNewStreamingExecuteResponse_PreservesDeliveryMode(t *testing.T) {
	resp := NewStreamingExecuteResponse(compatibility.NewSliceEventStream([]compatibility.OutputEvent{
		{Kind: compatibility.OutputEventStarted, ResultID: "resp_1", Model: "m"},
	}))

	if got := resp.DeliveryMode(); got != compatibility.DeliveryModeStreaming {
		t.Fatalf("delivery mode = %q, want %q", got, compatibility.DeliveryModeStreaming)
	}
	if resp.Stream() == nil {
		t.Fatal("stream = nil, want non-nil")
	}
}
