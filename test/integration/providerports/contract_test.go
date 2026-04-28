package providerports

import (
	"context"
	"errors"
	"testing"

	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/ports"
)

func TestProviderExecutorContract_CarriesCanonicalSemanticRequestAndBackendOrigin(t *testing.T) {
	executor := fakeExecutor{
		err: compatibility.NewBackendError("backend-a", 429, "rate limited", "60"),
	}

	_, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewDialogRequest(
			"m",
			[]compatibility.CanonicalItem{compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi")},
		), ports.NewExecutionContract(compatibility.DeliveryModeStreaming),
		ports.NewRoutableTarget(
			"backend-a",
			"custom",
			"http://localhost:8080/v1",
			"cred-1",
			"chat_completions", "", ""),
	))
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var backendErr compatibility.BackendError
	if !errors.As(err, &backendErr) {
		t.Fatalf("expected compatibility.BackendError, got %T", err)
	}
	if backendErr.Origin != compatibility.ErrorOriginBackend {
		t.Fatalf("origin = %q, want %q", backendErr.Origin, compatibility.ErrorOriginBackend)
	}
	if backendErr.RetryAfterHeaderValue != "60" {
		t.Fatalf("retry_after = %q, want %q", backendErr.RetryAfterHeaderValue, "60")
	}
}

func TestProviderExecutorContract_CarriesStreamingResponsesWithoutVendorTypes(t *testing.T) {
	executor := fakeExecutor{
		resp: ports.NewStreamingExecuteResponse(
			compatibility.NewSliceEventStream([]compatibility.OutputEvent{
				{Kind: compatibility.OutputEventStarted, ResultID: "resp_1", Model: "m"},
				{Kind: compatibility.OutputEventItemStarted, ItemKind: compatibility.OutputItemText, ItemID: "text_0"},
				{Kind: compatibility.OutputEventTextDelta, ItemID: "text_0", TextDelta: "hello"},
				{Kind: compatibility.OutputEventItemCompleted, ItemKind: compatibility.OutputItemText, ItemID: "text_0"},
				{Kind: compatibility.OutputEventCompleted, FinishReason: "completed"},
			}),
		),
	}

	resp, err := executor.Execute(context.Background(), ports.NewExecuteRequest(
		compatibility.NewGenerationRequest(
			compatibility.GenerationRequestParams{
				Model:     "m",
				InputText: "hi",
			},
		), ports.NewExecutionContract(compatibility.DeliveryModeStreaming),
		ports.NewRoutableTarget(
			"backend-a",
			"custom",
			"http://localhost:8080/v1",
			"cred-1",
			"responses", "", ""),
	))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if got := resp.DeliveryMode(); got != compatibility.DeliveryModeStreaming {
		t.Fatalf("delivery mode = %q, want %q", got, compatibility.DeliveryModeStreaming)
	}
	event, err := resp.Stream().Next()
	if err != nil {
		t.Fatalf("stream next: %v", err)
	}
	if got := event.Kind; got != compatibility.OutputEventStarted {
		t.Fatalf("event kind = %q, want %q", got, compatibility.OutputEventStarted)
	}
}

type fakeExecutor struct {
	resp ports.ExecuteResponse
	err  error
}

func (e fakeExecutor) Execute(context.Context, ports.ExecuteRequest) (ports.ExecuteResponse, error) {
	return e.resp, e.err
}
