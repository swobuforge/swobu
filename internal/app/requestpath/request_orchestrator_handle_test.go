package requestpath

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
	"github.com/swobuforge/swobu/internal/ports"
)

func TestHandle_ContinuationRecoveryRetryIsOwnedByAttemptPipeline(t *testing.T) {
	endpoint := testResponsesEndpoint(t)
	reader := endpointReaderStub{endpoint: endpoint}
	providers := &scriptedProviderExecutor{
		steps: []providerStep{
			{
				err: compatibility.NewBackendError(
					"backend-a",
					400,
					`{"error":{"message":"Previous response with id 'resp_missing' not found.","type":"invalid_request_error","param":"previous_response_id","code":"previous_response_not_found"}}`,
					"",
				),
			},
			{
				resp: ports.NewBufferedExecuteResponse(
					compatibility.NewConversationOutput("resp_1", "m", []compatibility.OutputItem{
						compatibility.NewTextOutputItem("text_0", "ok"),
					}, "completed"),
				),
			},
		},
	}

	handler := NewRequestHandler(reader, providers, nil, nil)
	out, err := handler.Handle(context.Background(), HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req_1",
		Request: compatibility.NewDialogRequest("m", []compatibility.CanonicalItem{
			compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi"),
		}),
		Contract: NewExecutionContract(compatibility.DeliveryModeBuffered),
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if got := len(providers.calls); got != 2 {
		t.Fatalf("provider calls = %d, want 2", got)
	}
	first, ok := providers.calls[0].Request.(compatibility.GenerationCanonicalRequest)
	if !ok {
		t.Fatalf("first request type = %T, want GenerationCanonicalRequest", providers.calls[0].Request)
	}
	second, ok := providers.calls[1].Request.(compatibility.GenerationCanonicalRequest)
	if !ok {
		t.Fatalf("second request type = %T, want GenerationCanonicalRequest", providers.calls[1].Request)
	}
	if !first.HasLastTurn() {
		t.Fatal("first request should include last_turn for continuation-aware realization")
	}
	if second.HasLastTurn() {
		t.Fatal("second request should use full-thread fallback without last_turn optimization")
	}
	metadata := out.Response.Metadata()
	if metadata.AttemptCount != 2 {
		t.Fatalf("attempt_count = %d, want 2", metadata.AttemptCount)
	}
	if !metadata.ContinuityRecovered {
		t.Fatal("continuity_recovered = false, want true")
	}
	if metadata.ContinuityRecoveryTrigger != "previous_response_not_found" {
		t.Fatalf("continuity_recovery_trigger = %q, want %q", metadata.ContinuityRecoveryTrigger, "previous_response_not_found")
	}
}

func TestHandle_ImmediateToolModeDowngradeRetryAppliesInSameRequest(t *testing.T) {
	endpoint := testResponsesEndpoint(t)
	reader := endpointReaderStub{endpoint: endpoint}
	providers := &toolModeAwareProvider{}
	handler := NewRequestHandler(reader, providers, nil, nil)

	request := compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{
		Model: "m",
		Items: []compatibility.CanonicalItem{
			compatibility.NewTextItem(compatibility.ItemAuthorUser, "use tool"),
		},
		ToolMode: compatibility.ToolModeRequired,
	})
	out, err := handler.Handle(context.Background(), HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req_strict_1",
		Request:      request,
		Contract:     NewExecutionContract(compatibility.DeliveryModeBuffered),
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if got := out.Response.Output().Items(); len(got) != 1 || got[0].Text != "auto_ok" {
		t.Fatalf("response items = %#v, want one auto_ok text item", got)
	}
	if got := len(providers.modes); got != 2 {
		t.Fatalf("provider mode calls = %d, want 2", got)
	}
	if providers.modes[0] != compatibility.ToolModeRequired {
		t.Fatalf("first provider tool mode = %q, want %q", providers.modes[0], compatibility.ToolModeRequired)
	}
	if providers.modes[1] != compatibility.ToolModeAuto {
		t.Fatalf("second provider tool mode = %q, want %q", providers.modes[1], compatibility.ToolModeAuto)
	}
	if got := out.Response.Metadata().AttemptCount; got != 2 {
		t.Fatalf("attempt_count = %d, want 2", got)
	}
}

func TestHandle_ToolModeDowngradeRetryRequiresCapabilityFlag(t *testing.T) {
	endpoint := testChatCompletionsEndpoint(t)
	reader := endpointReaderStub{endpoint: endpoint}
	providers := &toolModeAwareProvider{}
	handler := NewRequestHandler(reader, providers, nil, nil)

	request := compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{
		Model: "m",
		Items: []compatibility.CanonicalItem{
			compatibility.NewTextItem(compatibility.ItemAuthorUser, "use tool"),
		},
		ToolMode: compatibility.ToolModeRequired,
	})
	_, err := handler.Handle(context.Background(), HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req_strict_chat_1",
		Request:      request,
		Contract:     NewExecutionContract(compatibility.DeliveryModeBuffered),
	})
	if err == nil {
		t.Fatal("expected backend error when capability does not allow immediate downgrade retry")
	}
	if got := len(providers.modes); got != 1 {
		t.Fatalf("provider mode calls = %d, want 1", got)
	}
	if providers.modes[0] != compatibility.ToolModeRequired {
		t.Fatalf("provider tool mode = %q, want %q", providers.modes[0], compatibility.ToolModeRequired)
	}
}

func TestHandle_ToolModeDowngradeRetryUsesModelSpecificCapabilityOverride(t *testing.T) {
	endpoint := testProviderChatStrictToolModelEndpoint(t)
	reader := endpointReaderStub{endpoint: endpoint}
	providers := &toolModeAwareProvider{}
	handler := NewRequestHandler(reader, providers, nil, nil)

	request := compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{
		Model: "nvidia/nemotron-3-super-120b-a12b",
		Items: []compatibility.CanonicalItem{
			compatibility.NewTextItem(compatibility.ItemAuthorUser, "use tool"),
		},
		ToolMode: compatibility.ToolModeRequired,
	})
	out, err := handler.Handle(context.Background(), HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req_strict_openrouter_chat_1",
		Request:      request,
		Contract:     NewExecutionContract(compatibility.DeliveryModeBuffered),
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if got := len(providers.modes); got != 2 {
		t.Fatalf("provider mode calls = %d, want 2", got)
	}
	if providers.modes[0] != compatibility.ToolModeRequired {
		t.Fatalf("first provider tool mode = %q, want %q", providers.modes[0], compatibility.ToolModeRequired)
	}
	if providers.modes[1] != compatibility.ToolModeAuto {
		t.Fatalf("second provider tool mode = %q, want %q", providers.modes[1], compatibility.ToolModeAuto)
	}
	if got := out.Response.Metadata().AttemptCount; got != 2 {
		t.Fatalf("attempt_count = %d, want 2", got)
	}
}

type endpointReaderStub struct {
	endpoint endpointintent.Endpoint
}

func (s endpointReaderStub) GetEndpoint(context.Context, endpointintent.EndpointName) (endpointintent.Endpoint, error) {
	return s.endpoint, nil
}

type providerStep struct {
	resp ports.ExecuteResponse
	err  error
}

type scriptedProviderExecutor struct {
	steps []providerStep
	calls []ports.ExecuteRequest
}

func (s *scriptedProviderExecutor) Execute(_ context.Context, req ports.ExecuteRequest) (ports.ExecuteResponse, error) {
	s.calls = append(s.calls, req)
	if len(s.steps) == 0 {
		return ports.ExecuteResponse{}, compatibility.InternalError("missing scripted provider step")
	}
	step := s.steps[0]
	s.steps = s.steps[1:]
	if step.err != nil {
		return ports.ExecuteResponse{}, step.err
	}
	return step.resp, nil
}

type toolModeAwareProvider struct {
	modes []compatibility.ToolMode
}

func (p *toolModeAwareProvider) Execute(_ context.Context, req ports.ExecuteRequest) (ports.ExecuteResponse, error) {
	typed, ok := req.Request.(compatibility.GenerationCanonicalRequest)
	if !ok {
		return ports.ExecuteResponse{}, compatibility.BadRequest("response request is required")
	}
	p.modes = append(p.modes, typed.ToolMode())
	if typed.ToolMode() == compatibility.ToolModeRequired {
		backendErr := compatibility.NewBackendError(
			req.Target.BackendRef,
			400,
			`{"error":{"message":"tool_choice required is unsupported","type":"invalid_request_error","param":"tool_choice","code":"unsupported_parameter"}}`,
			"",
		)
		return ports.ExecuteResponse{}, compatibility.NewClassifiedBackendError(compatibility.BackendErrorClassToolChoiceUnsupported, backendErr)
	}
	return ports.NewBufferedExecuteResponse(
		compatibility.NewConversationOutput("resp_auto", typed.Model(), []compatibility.OutputItem{
			compatibility.NewTextOutputItem("text_0", "auto_ok"),
		}, "completed"),
	), nil
}

func testResponsesEndpoint(t *testing.T) endpointintent.Endpoint {
	t.Helper()

	name, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	ref, err := endpointintent.ParseProviderConfigRef("backend-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("custom")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	config, err := endpointintent.NewProviderConfig(ref, spec, "https://example.test/v1", "", protocolsurface.Responses)
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	config, err = config.WithModelID("m")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	return mustEndpoint(t, name, []endpointintent.ProviderConfig{config}, ref)
}

func testChatCompletionsEndpoint(t *testing.T) endpointintent.Endpoint {
	t.Helper()

	name, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	ref, err := endpointintent.ParseProviderConfigRef("backend-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("custom")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	config, err := endpointintent.NewProviderConfig(ref, spec, "https://example.test/v1", "", protocolsurface.ChatCompletions)
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	config, err = config.WithModelID("m")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	return mustEndpoint(t, name, []endpointintent.ProviderConfig{config}, ref)
}

func testProviderChatStrictToolModelEndpoint(t *testing.T) endpointintent.Endpoint {
	t.Helper()

	name, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	ref, err := endpointintent.ParseProviderConfigRef("backend-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("openrouter")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	config, err := endpointintent.NewProviderConfig(ref, spec, "https://example.test/v1", "", protocolsurface.ChatCompletions)
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	config, err = config.WithModelID("nvidia/nemotron-3-super-120b-a12b")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	return mustEndpoint(t, name, []endpointintent.ProviderConfig{config}, ref)
}

func mustEndpoint(
	t *testing.T,
	name endpointintent.EndpointName,
	configs []endpointintent.ProviderConfig,
	selected endpointintent.ProviderConfigRef,
) endpointintent.Endpoint {
	t.Helper()
	endpoint, err := endpointintent.NewEndpoint(name, configs, selected)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	return endpoint
}
