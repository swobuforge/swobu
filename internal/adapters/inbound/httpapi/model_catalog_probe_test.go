package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

type stubTargetProber struct {
	result   provider.TargetProbeResult
	err      error
	probeFn  func(provider.TargetSnapshot) (provider.TargetProbeResult, error)
	attempts []provider.TargetSnapshot
}

func (s *stubTargetProber) ProbeTarget(_ context.Context, target provider.TargetSnapshot) (provider.TargetProbeResult, error) {
	s.attempts = append(s.attempts, target)
	if s.probeFn != nil {
		return s.probeFn(target)
	}
	return s.result, s.err
}

func postTargetProbe(t *testing.T, handler TargetProbeHandler, connection workspaceapi.Connection, protocol string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(targetProbeRequest{Connection: connection, ProviderProtocol: protocol})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/_swobu/target-probe", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestModelCatalogProbeHandlerRequiresTypedPOST(t *testing.T) {
	h := NewTargetProbeHandler(&stubTargetProber{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_swobu/target-probe?provider_spec=openai", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestModelCatalogProbeHandlerRejectsTrailingJSON(t *testing.T) {
	h := NewTargetProbeHandler(&stubTargetProber{})
	req := httptest.NewRequest(http.MethodPost, "/_swobu/target-probe", strings.NewReader(`{"connection":{}} {}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestModelCatalogProbeHandlerCarriesConnectionAndOpaqueDiagnostics(t *testing.T) {
	diagnostics := json.RawMessage(`{"authentication":"aws_identity"}`)
	stub := &stubTargetProber{result: provider.TargetProbeResult{
		Deployments: []profile.ProviderDeploymentRecord{{Name: "model-1"}},
		Diagnostics: diagnostics,
	}}
	rec := postTargetProbe(t, NewTargetProbeHandler(stub), workspaceapi.Connection{
		Bedrock: &workspaceapi.BedrockConnection{Region: "eu-west-2", Credential: "env:AWS_BEARER_TOKEN_BEDROCK"},
	}, "responses")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(stub.attempts) != 1 ||
		stub.attempts[0].ProviderSpec != "bedrock" ||
		stub.attempts[0].BaseURL != "" ||
		stub.attempts[0].BedrockRegion() != "eu-west-2" ||
		stub.attempts[0].CredentialRef != "env:AWS_BEARER_TOKEN_BEDROCK" {
		t.Fatalf("attempts = %#v", stub.attempts)
	}
	var out TargetProbeResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if string(out.Diagnostics) != string(diagnostics) || len(out.Deployments) != 1 {
		t.Fatalf("result = %#v", out)
	}
}

func TestModelCatalogProbeHandlerCustomConnectionPreservesHeaderAuth(t *testing.T) {
	stub := &stubTargetProber{result: provider.TargetProbeResult{Deployments: []profile.ProviderDeploymentRecord{{Name: "model-1"}}}}
	rec := postTargetProbe(t, NewTargetProbeHandler(stub), workspaceapi.Connection{
		Custom: &workspaceapi.CustomConnection{BaseURL: "https://example.test/v1", Header: &workspaceapi.CustomHeader{Name: "X-Custom-Auth", Credential: "env:CUSTOM_KEY"}},
	}, "responses")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(stub.attempts) != 1 || stub.attempts[0].ProviderSpec != "custom" || stub.attempts[0].AuthHeader() != "X-Custom-Auth" {
		t.Fatalf("attempt = %#v", stub.attempts)
	}
}

func TestModelCatalogProbeHandlerAutoProbeUsesProtocolCapabilities(t *testing.T) {
	stub := &stubTargetProber{probeFn: func(target provider.TargetSnapshot) (provider.TargetProbeResult, error) {
		if target.ProviderProtocol == "responses_stream" {
			return provider.TargetProbeResult{Deployments: []profile.ProviderDeploymentRecord{{Name: "model-1"}}}, nil
		}
		return provider.TargetProbeResult{}, errors.New("try next protocol")
	}}
	rec := postTargetProbe(t, NewTargetProbeHandler(stub), workspaceapi.Connection{
		OpenAI: &workspaceapi.CredentialConnection{Credential: "env:OPENAI_API_KEY"},
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out TargetProbeResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.ResolvedProviderProtocol != "responses_stream" || len(stub.attempts) < 2 {
		t.Fatalf("result=%#v attempts=%d", out, len(stub.attempts))
	}
}

func TestModelCatalogProbeHandlerRejectsInvalidConnectionUnion(t *testing.T) {
	rec := postTargetProbe(t, NewTargetProbeHandler(&stubTargetProber{}), workspaceapi.Connection{}, "responses")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "exactly one provider variant") {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestModelCatalogProbeHandlerNormalizesFileCredentialResolutionError(t *testing.T) {
	stub := &stubTargetProber{err: errors.New("BAD_ENDPOINT: credential reference could not be resolved")}
	rec := postTargetProbe(t, NewTargetProbeHandler(stub), workspaceapi.Connection{
		OpenAI: &workspaceapi.CredentialConnection{Credential: "file:/missing/key"},
	}, "responses")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "credential file could not be resolved") {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}
