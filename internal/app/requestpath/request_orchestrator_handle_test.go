package requestpath

import (
	"context"
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/ports"
)

func TestHandle_ContinuationRecoveryRetryIsOwnedByAttemptPipeline(t *testing.T) {
	endpoint := testResponsesEndpoint(t)
	reader := endpointReaderStub{endpoint: endpoint}
	providers := &scriptedProviderExecutor{
		steps: []providerStep{
			{
				err: canonical.NewBackendError(
					"backend-a",
					400,
					`{"error":{"message":"Previous response with id 'resp_missing' not found.","type":"invalid_request_error","param":"previous_response_id","code":"previous_response_not_found"}}`,
					"",
				),
			},
			{
				resp: ports.NewBufferedProviderResponse(
					canonical.NewConversationOutput("resp_1", "m", []canonical.OutputItem{
						canonical.NewTextOutputItem("text_0", "ok"),
					}, "completed"),
				),
			},
		},
	}

	handler := NewRequestHandler(reader, providers, nil, nil)
	continuity := continuationStoreStub{
		snapshot: canonical.NewContinuitySnapshot("resp_missing", "m", nil),
	}
	handler = NewRequestHandler(reader, providers, nil, continuity)
	out, err := handler.Handle(context.Background(), HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req_1",
		Request: canonical.NewGenerationRequest(canonical.GenerationRequestParams{
			Model:              "m",
			Items:              []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
			PreviousResponseID: "resp_missing",
		}),
		Contract: NewExecutionContract(false),
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if got := len(providers.calls); got != 2 {
		t.Fatalf("provider calls = %d, want 2", got)
	}
	first, ok := providers.calls[0].Request.(canonical.GenerationCanonicalRequest)
	if !ok {
		t.Fatalf("first request type = %T, want GenerationCanonicalRequest", providers.calls[0].Request)
	}
	second, ok := providers.calls[1].Request.(canonical.GenerationCanonicalRequest)
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

func TestHandle_ToolModeDowngradeRetryRequiresCapabilityFlag(t *testing.T) {
	endpoint := testChatCompletionsEndpoint(t)
	reader := endpointReaderStub{endpoint: endpoint}
	providers := &toolModeAwareProvider{}
	handler := NewRequestHandler(reader, providers, nil, nil)

	request := canonical.NewGenerationRequest(canonical.GenerationRequestParams{
		Model: "m",
		Items: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorUser, "hi"),
		},
		ToolMode: canonical.ToolModeRequired,
	})
	out, err := handler.Handle(context.Background(), HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req_strict_chat_1",
		Request:      request,
		Contract:     NewExecutionContract(false),
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	closed, err := canonical.ReadClosedEnvelope(context.Background(), out.Response.EnvelopeStream(), canonical.EnvResponse)
	if err != nil {
		t.Fatalf("ReadClosedEnvelope returned error: %v", err)
	}
	output, err := closed.ProjectResponse()
	if err != nil {
		t.Fatalf("ProjectResponse returned error: %v", err)
	}
	if got := output.Items(); len(got) != 1 || got[0].Text != "auto_ok" {
		t.Fatalf("response items = %#v, want one auto_ok text item", got)
	}
	if got := len(providers.modes); got != 2 {
		t.Fatalf("provider mode calls = %d, want 2", got)
	}
	if providers.modes[0] != canonical.ToolModeRequired {
		t.Fatalf("first provider tool mode = %q, want %q", providers.modes[0], canonical.ToolModeRequired)
	}
	if providers.modes[1] != canonical.ToolModeAuto {
		t.Fatalf("second provider tool mode = %q, want %q", providers.modes[1], canonical.ToolModeAuto)
	}
	if got := out.Response.Metadata().AttemptCount; got != 2 {
		t.Fatalf("attempt_count = %d, want 2", got)
	}
}

func TestHandle_ToolModeDowngradeRetryUsesModelSpecificCapabilityOverride(t *testing.T) {
	endpoint := testProviderChatStrictToolModelEndpoint(t)
	reader := endpointReaderStub{endpoint: endpoint}
	providers := &toolModeAwareProvider{}
	handler := NewRequestHandler(reader, providers, nil, nil)

	request := canonical.NewGenerationRequest(canonical.GenerationRequestParams{
		Model: "nvidia/nemotron-3-super-120b-a12b",
		Items: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorUser, "use tool"),
		},
		ToolMode: canonical.ToolModeRequired,
	})
	out, err := handler.Handle(context.Background(), HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req_strict_openrouter_chat_1",
		Request:      request,
		Contract:     NewExecutionContract(false),
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if got := len(providers.modes); got != 2 {
		t.Fatalf("provider mode calls = %d, want 2", got)
	}
	if providers.modes[0] != canonical.ToolModeRequired {
		t.Fatalf("first provider tool mode = %q, want %q", providers.modes[0], canonical.ToolModeRequired)
	}
	if providers.modes[1] != canonical.ToolModeAuto {
		t.Fatalf("second provider tool mode = %q, want %q", providers.modes[1], canonical.ToolModeAuto)
	}
	if got := out.Response.Metadata().AttemptCount; got != 2 {
		t.Fatalf("attempt_count = %d, want 2", got)
	}
}

func TestHandle_PreCommitFallbackDisabled_DoesNotTrySecondaryProvider(t *testing.T) {
	endpoint := testDualProviderResponsesEndpoint(t)
	reader := endpointReaderStub{endpoint: endpoint}
	providers := &scriptedProviderExecutor{
		steps: []providerStep{
			{err: canonical.NewBackendError("backend-a", 503, "upstream down", "")},
			{resp: ports.NewBufferedProviderResponse(canonical.NewConversationOutput("resp_fallback", "m2", []canonical.OutputItem{canonical.NewTextOutputItem("text_0", "ok")}, "completed"))},
		},
	}
	handler := NewRequestHandler(reader, providers, nil, nil)
	_, err := handler.Handle(context.Background(), HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req_no_fallback",
		Request: canonical.NewGenerationRequest(canonical.GenerationRequestParams{
			Model: "m1",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		}),
		Contract: NewExecutionContract(false),
	})
	if err == nil {
		t.Fatal("expected error without fallback")
	}
	if got := len(providers.calls); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}
}

func TestHandle_PreCommitFallbackEnabled_TriesSecondaryProvider(t *testing.T) {
	endpoint := testDualProviderResponsesEndpoint(t)
	reader := endpointReaderStub{endpoint: endpoint}
	providers := &scriptedProviderExecutor{
		steps: []providerStep{
			{err: canonical.NewBackendError("backend-a", 503, "upstream down", "")},
			{resp: ports.NewBufferedProviderResponse(canonical.NewConversationOutput("resp_fallback", "m2", []canonical.OutputItem{canonical.NewTextOutputItem("text_0", "ok")}, "completed"))},
		},
	}
	handler := NewRequestHandler(reader, providers, nil, nil)
	out, err := handler.Handle(context.Background(), HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req_with_fallback",
		Request: canonical.NewGenerationRequest(canonical.GenerationRequestParams{
			Model: "m1",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		}),
		Contract: NewExecutionContractWithPreCommitFallback(false),
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if got := len(providers.calls); got != 2 {
		t.Fatalf("provider calls = %d, want 2", got)
	}
	if got := providers.calls[0].Target.BackendRef; got != "backend-a" {
		t.Fatalf("first attempt backend ref = %q, want %q", got, "backend-a")
	}
	if got := providers.calls[1].Target.BackendRef; got != "backend-b" {
		t.Fatalf("second attempt backend ref = %q, want %q", got, "backend-b")
	}
	if got := out.Target.BackendRef; got != "backend-b" {
		t.Fatalf("final backend ref = %q, want %q", got, "backend-b")
	}
	if got := out.Response.Metadata().ModelResolved; got != "m2" {
		t.Fatalf("model_resolved = %q, want %q", got, "m2")
	}
}

func TestHandle_PreCommitFallbackEnabled_AllRoutesFail_ReturnsLastError(t *testing.T) {
	endpoint := testDualProviderResponsesEndpoint(t)
	reader := endpointReaderStub{endpoint: endpoint}
	last := canonical.NewBackendError("backend-b", 429, "rate limited", "")
	providers := &scriptedProviderExecutor{
		steps: []providerStep{
			{err: canonical.NewBackendError("backend-a", 503, "upstream down", "")},
			{err: last},
		},
	}
	handler := NewRequestHandler(reader, providers, nil, nil)
	_, err := handler.Handle(context.Background(), HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req_with_fallback_all_fail",
		Request: canonical.NewGenerationRequest(canonical.GenerationRequestParams{
			Model: "m1",
			Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
		}),
		Contract: NewExecutionContractWithPreCommitFallback(true),
	})
	if err == nil {
		t.Fatal("expected error when all fallback routes fail")
	}
	if got := len(providers.calls); got != 2 {
		t.Fatalf("provider calls = %d, want 2", got)
	}
	var typed canonical.BackendError
	ok := errors.As(err, &typed)
	if !ok {
		t.Fatalf("error type = %T, want backend error", err)
	}
	if typed.BackendRef != "backend-b" {
		t.Fatalf("backend ref = %q, want %q", typed.BackendRef, "backend-b")
	}
	if typed.StatusCode != 429 {
		t.Fatalf("status code = %d, want %d", typed.StatusCode, 429)
	}
}

type endpointReaderStub struct {
	endpoint endpointintent.Endpoint
}

func (s endpointReaderStub) GetEndpoint(context.Context, endpointintent.EndpointName) (endpointintent.Endpoint, error) {
	return s.endpoint, nil
}

type providerStep struct {
	resp ports.ProviderResponse
	err  error
}

type scriptedProviderExecutor struct {
	steps []providerStep
	calls []ports.ProviderRequest
}

func (s *scriptedProviderExecutor) Execute(_ context.Context, req ports.ProviderRequest) (ports.ProviderResponse, error) {
	s.calls = append(s.calls, req)
	if len(s.steps) == 0 {
		return ports.ProviderResponse{}, canonical.InternalError("missing scripted provider step")
	}
	step := s.steps[0]
	s.steps = s.steps[1:]
	if step.err != nil {
		return ports.ProviderResponse{}, step.err
	}
	return step.resp, nil
}

type toolModeAwareProvider struct {
	modes []canonical.ToolMode
}

type continuationStoreStub struct {
	snapshot canonical.ContinuitySnapshot
}

func (s continuationStoreStub) Load(context.Context, string) (canonical.ContinuitySnapshot, bool, error) {
	return s.snapshot.Clone(), true, nil
}

func (s continuationStoreStub) MatchPrefix(context.Context, canonical.ContinuationNamespace, []canonical.CanonicalItem) (canonical.ContinuationPrefixMatch, bool, error) {
	return canonical.ContinuationPrefixMatch{}, false, nil
}

func (s continuationStoreStub) Store(context.Context, canonical.ContinuationNamespace, canonical.ContinuitySnapshot) error {
	return nil
}

func (p *toolModeAwareProvider) Execute(_ context.Context, req ports.ProviderRequest) (ports.ProviderResponse, error) {
	typed, ok := req.Request.(canonical.GenerationCanonicalRequest)
	if !ok {
		return ports.ProviderResponse{}, canonical.BadRequest("response request is required")
	}
	p.modes = append(p.modes, typed.ToolMode())
	if typed.ToolMode() == canonical.ToolModeRequired {
		backendErr := canonical.NewBackendError(
			req.Target.BackendRef,
			400,
			`{"error":{"message":"tool_choice required is unsupported","type":"invalid_request_error","param":"tool_choice","code":"unsupported_parameter"}}`,
			"",
		)
		return ports.ProviderResponse{}, canonical.NewClassifiedBackendError(canonical.BackendErrorClassToolChoiceUnsupported, backendErr)
	}
	return ports.NewBufferedProviderResponse(
		canonical.NewConversationOutput("resp_auto", typed.Model(), []canonical.OutputItem{
			canonical.NewTextOutputItem("text_0", "auto_ok"),
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
	spec, err := endpointintent.ParseProviderSpec("openai_compatible")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	config, err := endpointintent.NewProviderConfig(ref, spec, "https://example.test/v1", "")
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
	spec, err := endpointintent.ParseProviderSpec("openai_compatible")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	config, err := endpointintent.NewProviderConfig(ref, spec, "https://example.test/v1", "")
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	config, err = config.WithModelID("m")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	return mustEndpoint(t, name, []endpointintent.ProviderConfig{config}, ref)
}

func testDualProviderResponsesEndpoint(t *testing.T) endpointintent.Endpoint {
	t.Helper()

	name, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	refA, err := endpointintent.ParseProviderConfigRef("backend-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	refB, err := endpointintent.ParseProviderConfigRef("backend-b")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("openai_compatible")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	configA, err := endpointintent.NewProviderConfig(refA, spec, "https://a.test/v1", "")
	if err != nil {
		t.Fatalf("NewProviderConfig A returned error: %v", err)
	}
	configA, err = configA.WithModelID("m1")
	if err != nil {
		t.Fatalf("WithModelID A returned error: %v", err)
	}
	configB, err := endpointintent.NewProviderConfig(refB, spec, "https://b.test/v1", "")
	if err != nil {
		t.Fatalf("NewProviderConfig B returned error: %v", err)
	}
	configB, err = configB.WithModelID("m2")
	if err != nil {
		t.Fatalf("WithModelID B returned error: %v", err)
	}
	return mustEndpoint(t, name, []endpointintent.ProviderConfig{configA, configB}, refA)
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
	config, err := endpointintent.NewProviderConfig(ref, spec, "https://example.test/v1", "")
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
