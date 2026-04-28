package app_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/metrofun/swobu/internal/app/requestpath"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/domain/endpointintent"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/internal/domain/runtimeevidence"
	"github.com/metrofun/swobu/internal/ports"
)

func TestRequestPathHandler_PreservesErrorOriginAndRecordsTerminalEvidence(t *testing.T) {
	endpoint := testEndpoint(t)
	reader := fakeEndpointReader{endpoint: endpoint}
	evidence := &capturingEvidenceSink{}

	tests := []struct {
		name       string
		err        error
		wantResult runtimeevidence.ResultClass
		wantStatus int
	}{
		{
			name:       "backend error",
			err:        compatibility.NewBackendError("backend-a", 429, "rate limited", "60"),
			wantResult: runtimeevidence.ResultClassBackendError,
			wantStatus: 429,
		},
		{
			name:       "swobu error",
			err:        compatibility.UnsupportedOperation("not supported"),
			wantResult: runtimeevidence.ResultClassUnsupportedOperation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evidence.reset()
			orchestrator := requestpath.NewRequestHandler(reader, &fakeProviderExecutor{err: tt.err}, evidence, nil)

			_, err := orchestrator.Handle(context.Background(), requestpath.HandleInput{
				EndpointName: endpoint.Name(),
				RequestID:    "req-1",
				Request: compatibility.NewDialogRequest(
					"m",
					[]compatibility.CanonicalItem{compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi")},
				),
				Contract: requestpath.NewExecutionContract(compatibility.DeliveryModeStreaming),
			})
			if !errors.Is(err, tt.err) && err.Error() != tt.err.Error() {
				t.Fatalf("HandleRequest error = %v, want %v", err, tt.err)
			}
			if len(evidence.events) != 2 {
				t.Fatalf("events = %d, want 2", len(evidence.events))
			}
			got := evidence.events[1]
			if got.Result() != tt.wantResult {
				t.Fatalf("result class = %q, want %q", got.Result(), tt.wantResult)
			}
			if got.StatusCode() != tt.wantStatus {
				t.Fatalf("status code = %d, want %d", got.StatusCode(), tt.wantStatus)
			}
		})
	}
}

func TestRequestPathHandler_EmitsModelResolutionEvidenceFields(t *testing.T) {
	endpoint := testEndpoint(t)
	selected := endpoint.SelectedProviderConfig()
	selected, err := selected.WithModelID("model-from-swobu")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	endpoint, err = endpointintent.NewEndpoint(endpoint.Name(), []endpointintent.ProviderConfig{selected}, selected.Ref())
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	reader := fakeEndpointReader{endpoint: endpoint}
	evidence := &capturingEvidenceSink{}
	provider := &fakeProviderExecutor{
		resp: ports.NewBufferedExecuteResponse(
			compatibility.NewConversationOutput(
				"chatcmpl_1",
				"model-from-swobu",
				[]compatibility.OutputItem{compatibility.NewTextOutputItem("text_0", "ok")},
				"stop",
			),
		),
	}
	orchestrator := requestpath.NewRequestHandler(reader, provider, evidence, nil)

	_, err = orchestrator.Handle(context.Background(), requestpath.HandleInput{
		EndpointName: endpoint.Name(),
		RequestID:    "req-evidence-model",
		Request: compatibility.NewDialogRequest(
			"",
			[]compatibility.CanonicalItem{compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi")},
		),
		Contract: requestpath.NewExecutionContract(compatibility.DeliveryModeBuffered),
	})
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if len(evidence.events) != 2 {
		t.Fatalf("events len = %d, want 2", len(evidence.events))
	}
	for i, event := range evidence.events {
		if got := event.ModelRequested(); got != "" {
			t.Fatalf("event[%d] model requested = %q, want empty", i, got)
		}
		if got := event.ModelResolved(); got != "model-from-swobu" {
			t.Fatalf("event[%d] model resolved = %q, want %q", i, got, "model-from-swobu")
		}
		if got := event.ModelResolutionMode(); got != "default_missing" {
			t.Fatalf("event[%d] model resolution mode = %q, want %q", i, got, "default_missing")
		}
	}
}

func testEndpoint(t *testing.T) endpointintent.Endpoint {
	return testEndpointWithProtocol(t, "chat_completions")
}

func testEndpointWithProtocol(t *testing.T, protocolKind protocolsurface.Kind) endpointintent.Endpoint {
	t.Helper()

	name, err := endpointintent.ParseEndpointName("alpha")
	if err != nil {
		t.Fatalf("ParseEndpointName returned error: %v", err)
	}
	spec, err := endpointintent.ParseProviderSpec("custom")
	if err != nil {
		t.Fatalf("ParseProviderSpec returned error: %v", err)
	}
	ref, err := endpointintent.ParseProviderConfigRef("backend-a")
	if err != nil {
		t.Fatalf("ParseProviderConfigRef returned error: %v", err)
	}
	providerConfig, err := endpointintent.NewProviderConfig(
		ref,
		spec,
		"https://example.test/v1",
		"cred-1",
		protocolKind,
	)
	if err != nil {
		t.Fatalf("NewProviderConfig returned error: %v", err)
	}
	providerConfig, err = providerConfig.WithModelID("m")
	if err != nil {
		t.Fatalf("WithModelID returned error: %v", err)
	}
	endpoint, err := endpointintent.NewEndpoint(name, []endpointintent.ProviderConfig{providerConfig}, ref)
	if err != nil {
		t.Fatalf("NewEndpoint returned error: %v", err)
	}
	return endpoint
}

type fakeEndpointReader struct {
	endpoint  endpointintent.Endpoint
	endpoints []endpointintent.Endpoint
	err       error
}

func (r fakeEndpointReader) GetEndpoint(context.Context, endpointintent.EndpointName) (endpointintent.Endpoint, error) {
	return r.endpoint, r.err
}

func (r fakeEndpointReader) ListEndpoints(context.Context) ([]endpointintent.Endpoint, error) {
	if r.endpoints != nil {
		return r.endpoints, r.err
	}
	if r.endpoint.Name().IsZero() {
		return nil, r.err
	}
	return []endpointintent.Endpoint{r.endpoint}, r.err
}

type fakeProviderExecutor struct {
	got               ports.ExecuteRequest
	gotCatalogTargets []ports.RoutableTarget
	resp              ports.ExecuteResponse
	err               error
	modelsByBackend   map[string][]string
	modelCatalogErr   error
}

func (e *fakeProviderExecutor) Execute(_ context.Context, req ports.ExecuteRequest) (ports.ExecuteResponse, error) {
	e.got = req
	return e.resp, e.err
}

func (e *fakeProviderExecutor) ListModels(_ context.Context, target ports.RoutableTarget) ([]string, error) {
	e.gotCatalogTargets = append(e.gotCatalogTargets, target)
	if e.modelCatalogErr != nil {
		return nil, e.modelCatalogErr
	}
	return e.modelsByBackend[target.BackendRef], nil
}

type capturingEvidenceSink struct {
	events []runtimeevidence.TrafficEvent
}

func (s *capturingEvidenceSink) Append(_ context.Context, event runtimeevidence.TrafficEvent) {
	s.events = append(s.events, event)
}

func (s *capturingEvidenceSink) reset() {
	s.events = nil
}

type fakeContinuityStore struct {
	byResponseID map[string]compatibility.ContinuitySnapshot
	byNamespace  map[compatibility.ContinuationNamespace][]compatibility.ContinuitySnapshot
	stored       []compatibility.ContinuitySnapshot
}

func (s *fakeContinuityStore) Load(_ context.Context, previousResponseID string) (compatibility.ContinuitySnapshot, bool, error) {
	snapshot, ok := s.byResponseID[previousResponseID]
	return snapshot.Clone(), ok, nil
}

func (s *fakeContinuityStore) MatchPrefix(_ context.Context, namespace compatibility.ContinuationNamespace, thread []compatibility.CanonicalItem) (compatibility.ContinuationPrefixMatch, bool, error) {
	candidates := s.byNamespace[namespace]
	best := compatibility.ContinuationPrefixMatch{}
	bestOK := false
	for _, snapshot := range candidates {
		prefixLen := conversationPrefixLength(snapshot.Thread, thread)
		if !bestOK || prefixLen > best.PrefixLength {
			best = compatibility.ContinuationPrefixMatch{
				Snapshot:     snapshot.Clone(),
				PrefixLength: prefixLen,
			}
			bestOK = true
			continue
		}
		if bestOK && prefixLen == best.PrefixLength && prefixLen > 0 {
			best = compatibility.ContinuationPrefixMatch{
				Snapshot:     snapshot.Clone(),
				PrefixLength: prefixLen,
			}
		}
	}
	return best, bestOK, nil
}

func (s *fakeContinuityStore) Store(_ context.Context, namespace compatibility.ContinuationNamespace, snapshot compatibility.ContinuitySnapshot) error {
	if s.byResponseID == nil {
		s.byResponseID = map[string]compatibility.ContinuitySnapshot{}
	}
	if s.byNamespace == nil {
		s.byNamespace = map[compatibility.ContinuationNamespace][]compatibility.ContinuitySnapshot{}
	}
	cloned := snapshot.Clone()
	s.byResponseID[cloned.ResponseID] = cloned
	s.byNamespace[namespace] = append(s.byNamespace[namespace], cloned)
	s.stored = append(s.stored, cloned)
	return nil
}

func conversationPrefixLength(left []compatibility.CanonicalItem, right []compatibility.CanonicalItem) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	for i := 0; i < limit; i++ {
		if !reflect.DeepEqual(left[i], right[i]) {
			return i
		}
	}
	return limit
}
