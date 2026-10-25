package exchange

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestLocalToolDeliveryCheckpointFailureAfterVisibleText(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		name := "message"
		if mixed {
			name = "mixed-caller-handoff"
		}
		t.Run(name, func(t *testing.T) { testLocalToolDeliveryCheckpointFailure(t, mixed) })
	}
}

func testLocalToolDeliveryCheckpointFailure(t *testing.T, mixed bool) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	bridge := newLocalToolDelivery(cancel)
	id := canonical.SwobuResponseID("swobu_bridge_failure")
	bridge.projection = newLocalToolClientProjection(id)
	message, _ := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{canonical.NewTextMessagePart("visible answer")})
	response, err := canonical.NewCanonicalResponse(canonical.ResponseRef{SwobuID: id}, "fixture", []canonical.CanonicalItem{message}, canonical.Completed("stop"), canonical.NewUnknownTokenUsage())
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := response
	request := testCanonicalRequest("fixture")
	if mixed {
		localKey, _ := canonical.NewToolKey("workspace/mcp", canonical.ToolKindFunction, "docs__search")
		callerKey, _ := canonical.NewRequestToolKey(canonical.ToolKindFunction, "client_tool")
		localID, _ := canonical.NewToolCallID("local")
		callerID, _ := canonical.NewToolCallID("caller")
		object, _ := canonical.ParseJSONObject([]byte(`{}`))
		local, _ := canonical.NewToolCallItem(localID, localKey, canonical.NewJSONObjectToolInput(object))
		caller, _ := canonical.NewToolCallItem(callerID, callerKey, canonical.NewJSONObjectToolInput(object))
		localCall, _ := local.ToolCall()
		result, _ := canonical.NewToolResultItem(localID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("done")}, false)
		full, err := canonical.NewCanonicalResponse(response.Response(), response.Model(), []canonical.CanonicalItem{message, caller, local}, canonical.Completed("tool_calls"), response.Usage())
		if err != nil {
			t.Fatal(err)
		}
		handoff, err := prepareMixedToolHandoff(request, full, []canonical.ToolCallItem{localCall}, []canonical.CanonicalItem{result})
		if err != nil {
			t.Fatal(err)
		}
		response, checkpoint, request = handoff.client, handoff.response, handoff.request
		bridge.projection.ownsTool = func(key canonical.ToolKey) bool { return key == localKey }
	}
	puts := 0
	runner := runtimeBundle{CheckpointStore: failingCheckpointStore{puts: &puts}}
	call := providerCall{clientCodec: codecresolver.NewRuntimeCodecResolver().ClientCodec(canonical.ClientFamilyResponses), clientDelivery: delivery.StreamingDelivery(delivery.FramingSSE), exchangeID: "bridge_failure", workspaceSlug: "dev", fullRequest: request, executionAffinity: testExecutionAffinity("bridge-failure")}
	visible := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		defer close(bridge.events)
		for _, event := range canonical.SynthesizeResponseEnvelopeEvents(call.exchangeID, response.Response(), response.Model(), response.Items(), response.Completion(), response.Usage()) {
			if err := bridge.apply(ctx, call, runner, event); err != nil {
				finished <- err
				return
			}
		}
		select {
		case <-visible:
		case <-ctx.Done():
			finished <- ctx.Err()
			return
		}
		_, err := bridge.finish(ctx, call, runner, completedProviderResponse{client: response, continuation: checkpoint})
		finished <- err
	}()
	var out RequestOutput
	select {
	case out = <-bridge.ready:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	body := ClientTransportForTest(out.Response).Body
	defer body.Close()
	reader := bufio.NewReader(body)
	var raw strings.Builder
	for !strings.Contains(raw.String(), "visible answer") {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		raw.WriteString(line)
	}
	close(visible)
	tail, readErr := io.ReadAll(reader)
	raw.Write(tail)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	snapshot := responseCompletion(out.Response).Snapshot()
	var commitErr CheckpointCommitError
	if !errors.As(readErr, &commitErr) || snapshot.State != wire.CompletionFailed || puts != 1 {
		t.Fatalf("checkpoint failure lost: state=%v err=%v read=%v puts=%d", snapshot.State, snapshot.Err, readErr, puts)
	}
	if strings.Contains(raw.String(), "event: response.completed") || !strings.Contains(raw.String(), "response finalization failed locally") {
		t.Fatalf("partial response ended as success or lost sanitized failure: %s", raw.String())
	}
	if _, found, err := runner.CheckpointStore.Get(ctx, "dev", id); err != nil || found {
		t.Fatalf("failed checkpoint became resumable: found=%t err=%v", found, err)
	}
}
