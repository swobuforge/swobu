package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/swobuforge/swobu/internal/ports"
)

type stubModelCatalog struct {
	models       []string
	err          error
	validateUsed bool
	listFn       func(target ports.RoutableTarget) ([]string, error)
}

func (s *stubModelCatalog) ListModels(_ context.Context, target ports.RoutableTarget) ([]string, error) {
	if s.listFn != nil {
		return s.listFn(target)
	}
	if s.err != nil {
		return nil, s.err
	}
	return append([]string(nil), s.models...), nil
}

func (s *stubModelCatalog) ValidateCredentials(context.Context, ports.RoutableTarget) error {
	s.validateUsed = true
	return nil
}

func TestModelCatalogProbeHandler_LoadsModelIDsFromCatalogPath(t *testing.T) {
	stub := &stubModelCatalog{models: []string{"m1", "m2"}}
	h := NewModelCatalogProbeHandler(stub)

	query := url.Values{}
	query.Set("provider_spec", "bedrock")
	query.Set("base_url", "https://bedrock-runtime.us-east-1.amazonaws.com/openai/v1")
	query.Set("credential_ref", "env:AWS_BEARER_TOKEN_BEDROCK")
	req := httptest.NewRequest(http.MethodGet, "/_swobu/model-catalog?"+query.Encode(), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", rec.Code)
	}
	var out struct {
		ModelIDs []string `json:"model_ids"`
		Error    string   `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if out.Error != "" {
		t.Fatalf("probe error=%q", out.Error)
	}
	if len(out.ModelIDs) != 2 {
		t.Fatalf("model ids len=%d want 2", len(out.ModelIDs))
	}
	if stub.validateUsed {
		t.Fatal("probe should not call ValidateCredentials")
	}
}

func TestModelCatalogProbeHandler_ReturnsRawError(t *testing.T) {
	stub := &stubModelCatalog{err: errors.New("BAD_ENDPOINT: credential reference could not be resolved")}
	h := NewModelCatalogProbeHandler(stub)

	query := url.Values{}
	query.Set("provider_spec", "bedrock")
	query.Set("base_url", "https://bedrock-runtime.us-east-1.amazonaws.com/openai/v1")
	query.Set("credential_ref", "env:AWS_BEARER_TOKEN_BEDROCK")
	req := httptest.NewRequest(http.MethodGet, "/_swobu/model-catalog?"+query.Encode(), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", rec.Code)
	}
	var out struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if out.Error == "" {
		t.Fatal("expected probe error")
	}
}

func TestModelCatalogProbeHandler_AutoProbeTriesCapabilitiesOrderAndReturnsFirstSuccess(t *testing.T) {
	attempts := make([]string, 0, 4)
	stub := &stubModelCatalog{
		listFn: func(target ports.RoutableTarget) ([]string, error) {
			key := target.ProtocolKind.String() + "/" + target.SelectedFrame
			attempts = append(attempts, key)
			if key == "responses/sse_event" {
				return []string{"gpt-4.1-mini"}, nil
			}
			return nil, errors.New("probe failed for " + key)
		},
	}
	h := NewModelCatalogProbeHandler(stub)

	query := url.Values{}
	query.Set("provider_spec", "openai")
	query.Set("base_url", "https://api.openai.com/v1")
	query.Set("credential_ref", "env:OPENAI_API_KEY")
	req := httptest.NewRequest(http.MethodGet, "/_swobu/model-catalog?"+query.Encode(), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", rec.Code)
	}
	var out struct {
		ModelIDs                 []string `json:"model_ids"`
		Error                    string   `json:"error"`
		ResolvedProviderProtocol string   `json:"resolved_provider_protocol"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if out.Error != "" {
		t.Fatalf("unexpected probe error=%q", out.Error)
	}
	if out.ResolvedProviderProtocol != "responses_stream" {
		t.Fatalf("resolved provider variant=%q want responses_stream", out.ResolvedProviderProtocol)
	}
	if len(attempts) == 0 {
		t.Fatal("expected at least one probe attempt")
	}
	if attempts[0] != "responses/http_json_body" {
		t.Fatalf("first auto attempt=%q want responses/http_json_body", attempts[0])
	}
}
