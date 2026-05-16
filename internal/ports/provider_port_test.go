package ports

import (
	"context"
	"io"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestNewExecuteRequest_ClonesCanonicalRequestAndTargetInputs(t *testing.T) {
	request := canonical.NewDialogRequest(
		"m",
		[]canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorUser, "hi"),
			canonical.NewToolUseItem(canonical.ItemAuthorUser, "", "toolu_1", "calculator", map[string]any{"expr": "2+2"}),
		},
	)
	target := NewRoutableTarget(
		"backend-a",
		"openai_compatible",
		"http://localhost:8080/v1",
		"cred-1",
		"chat_completions",
		"",
	)
	req := NewProviderRequest(request, NewExecutionContract(true), target)

	requestItems := request.Items()
	requestItems[0].Text = "changed"
	requestItems[1].Input["expr"] = "changed"

	if got := req.Contract.ClientResponseMode; got != ResponseModeStreaming {
		t.Fatalf("client response mode = %v, want %v", got, ResponseModeStreaming)
	}
	if got := req.Contract.ProviderCallMode; got != ResponseModeStreaming {
		t.Fatalf("provider call mode = %v, want %v", got, ResponseModeStreaming)
	}
	if req.Contract.AllowPreCommitFallback {
		t.Fatal("allow_pre_commit_fallback = true, want false by default")
	}
	if got := req.Request.SemanticKind(); got != canonical.SemanticKindConversation {
		t.Fatalf("semantic kind = %q, want %q", got, canonical.SemanticKindConversation)
	}
	typed, ok := req.Request.(canonical.DialogCanonicalRequest)
	if !ok {
		t.Fatalf("expected canonical.DialogCanonicalRequest, got %T", req.Request)
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

func TestExecutionContract_WithPreCommitFallbackEnabled_SetsFlag(t *testing.T) {
	contract := NewExecutionContract(false)
	if contract.AllowPreCommitFallback {
		t.Fatal("allow_pre_commit_fallback = true, want false by default")
	}

	enabled := contract.WithPreCommitFallbackEnabled()
	if !enabled.AllowPreCommitFallback {
		t.Fatal("allow_pre_commit_fallback = false, want true after opt-in")
	}
	if enabled.ClientResponseMode != ResponseModeBuffered {
		t.Fatalf("client response mode = %v, want %v", enabled.ClientResponseMode, ResponseModeBuffered)
	}
	if enabled.ProviderCallMode != ResponseModeBuffered {
		t.Fatalf("provider call mode = %v, want %v", enabled.ProviderCallMode, ResponseModeBuffered)
	}
}

func TestExecutionContract_WithProviderCallMode_OverridesProviderModeOnly(t *testing.T) {
	contract := NewExecutionContract(false).WithProviderCallMode(ResponseModeStreaming)
	if contract.ClientResponseMode != ResponseModeBuffered {
		t.Fatalf("client response mode = %v, want %v", contract.ClientResponseMode, ResponseModeBuffered)
	}
	if contract.ProviderCallMode != ResponseModeStreaming {
		t.Fatalf("provider call mode = %v, want %v", contract.ProviderCallMode, ResponseModeStreaming)
	}
}

func TestNewBufferedExecuteResponse_ClonesCanonicalOutputAndPreservesDeliveryMode(t *testing.T) {
	output := canonical.NewConversationOutput(
		"resp_1",
		"m",
		[]canonical.OutputItem{
			canonical.NewTextOutputItem("text_0", "hi"),
		},
		"stop",
	)

	resp := NewBufferedProviderResponse(output)
	items := output.Items()
	items[0].Text = "changed"

	if resp.EnvelopeStream() == nil {
		t.Fatal("envelope stream = nil, want non-nil")
	}
	closed, err := canonical.ReadClosedEnvelope(context.Background(), resp.EnvelopeStream(), canonical.EnvResponse)
	if err != nil {
		t.Fatalf("ReadClosedEnvelope error: %v", err)
	}
	snapshot, err := closed.ProjectResponse()
	if err != nil {
		t.Fatalf("ProjectResponse error: %v", err)
	}
	if got := snapshot.Items()[0].Text; got != "hi" {
		t.Fatalf("output text = %q, want %q", got, "hi")
	}
}

func TestNewStreamingExecuteResponse_PreservesDeliveryMode(t *testing.T) {
	resp := NewEnvelopeStreamingProviderResponse(canonical.NewSliceEventReader([]canonical.Event{
		{ExchangeID: "test_exchange", Seq: 1, Kind: canonical.EventEnvelopeStart, EnvID: "res_1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}},
		{ExchangeID: "test_exchange", Seq: 2, Kind: canonical.EventEnvelopeEnd, EnvID: "res_1", Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted}},
	}))

	if resp.EnvelopeStream() == nil {
		t.Fatal("envelope stream = nil, want non-nil")
	}
}

func TestNewEnvelopeStreamingExecuteResponse_ProjectsLegacyStream(t *testing.T) {
	out := canonical.NewConversationOutput(
		"resp_env_1",
		"m",
		[]canonical.OutputItem{
			canonical.NewTextOutputItem("text_0", "ok"),
		},
		"completed",
	)
	reader, err := canonical.EventReaderFromCanonicalOutput("ex_ports_env", out)
	if err != nil {
		t.Fatalf("EventReaderFromCanonicalOutput error: %v", err)
	}

	resp := NewEnvelopeStreamingProviderResponse(reader)
	if resp.EnvelopeStream() == nil {
		t.Fatal("envelope stream = nil, want non-nil")
	}
	stream := resp.EnvelopeStream()
	seenResponseEnd := false
	for {
		ev, err := stream.Next(context.Background())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("legacy stream next error: %v", err)
		}
		if ev.Kind == canonical.EventEnvelopeEnd {
			payload, _ := ev.Payload.(canonical.EnvelopeEndPayload)
			if payload.Kind == canonical.EnvResponse {
				seenResponseEnd = true
			}
		}
	}
	if !seenResponseEnd {
		t.Fatal("expected response envelope end event")
	}
	if err := resp.Close(); err != nil {
		t.Fatalf("close error: %v", err)
	}
	if err := reader.Close(context.Background()); err != nil {
		t.Fatalf("reader close error: %v", err)
	}
}
