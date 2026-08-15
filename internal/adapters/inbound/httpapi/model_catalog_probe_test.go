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
		Options:     []profile.ModelAuthoringOption{{Name: "model-1"}},
		Diagnostics: diagnostics,
	}}
	rec := postTargetProbe(t, NewTargetProbeHandler(stub), workspaceapi.BedrockConnectionDocument("eu-west-2", "", "env:AWS_BEARER_TOKEN_BEDROCK"), "responses")
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
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if _, ok := wire["deployments"]; !ok {
		t.Fatalf("wire result omitted compatibility key deployments: %s", rec.Body.String())
	}
	if _, ok := wire["options"]; ok {
		t.Fatalf("wire result exposed internal options field: %s", rec.Body.String())
	}
	if string(out.Diagnostics) != string(diagnostics) || len(out.Options) != 1 {
		t.Fatalf("result = %#v", out)
	}
}

func TestModelCatalogProbeHandlerCustomConnectionPreservesHeaderAuth(t *testing.T) {
	stub := &stubTargetProber{result: provider.TargetProbeResult{Options: []profile.ModelAuthoringOption{{Name: "model-1"}}}}
	rec := postTargetProbe(t, NewTargetProbeHandler(stub), workspaceapi.CustomConnectionDocument("https://example.test/v1", &workspaceapi.CustomHeader{Name: "X-Custom-Auth", Credential: "env:CUSTOM_KEY"}), "responses")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(stub.attempts) != 1 || stub.attempts[0].ProviderSpec != "custom" || stub.attempts[0].AuthHeader() != "X-Custom-Auth" {
		t.Fatalf("attempt = %#v", stub.attempts)
	}
}

func TestModelCatalogProbeHandlerUsesNormalizedRunPodEndpoint(t *testing.T) {
	stub := &stubTargetProber{result: provider.TargetProbeResult{Options: []profile.ModelAuthoringOption{{Name: "served-model"}}}}
	rec := postTargetProbe(t, NewTargetProbeHandler(stub), workspaceapi.StandardConnection("runpod", "abc123", "env:RUNPOD_API_KEY"), "responses")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(stub.attempts) != 1 {
		t.Fatalf("attempts = %#v, want one probe", stub.attempts)
	}
	attempt := stub.attempts[0]
	if attempt.ProviderSpec != "runpod" || attempt.BaseURL != "https://api.runpod.ai/v2/abc123/openai/v1" || attempt.CredentialRef != "env:RUNPOD_API_KEY" {
		t.Fatalf("Runpod probe attempt = %#v", attempt)
	}
}

func TestModelCatalogProbeHandlerUsesFirstManifestProtocolWhenUnspecified(t *testing.T) {
	stub := &stubTargetProber{probeFn: func(target provider.TargetSnapshot) (provider.TargetProbeResult, error) {
		if target.ProviderProtocol != "responses" {
			t.Fatalf("selected protocol = %q, want static preferred responses", target.ProviderProtocol)
		}
		return provider.TargetProbeResult{}, errors.New("selected protocol rejected")
	}}
	rec := postTargetProbe(t, NewTargetProbeHandler(stub), workspaceapi.StandardConnection("openai", "", "env:OPENAI_API_KEY"), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out TargetProbeResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.ResolvedProviderProtocol != "" || len(stub.attempts) != 1 {
		t.Fatalf("result=%#v attempts=%d", out, len(stub.attempts))
	}
}

func TestModelCatalogProbeHandlerRejectsInvalidConnectionUnion(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/_swobu/target-probe", strings.NewReader(`{"connection":{},"provider_protocol":"responses"}`))
	rec := httptest.NewRecorder()
	NewTargetProbeHandler(&stubTargetProber{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "target probe request is invalid") {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestModelCatalogProbeHandlerNormalizesFileCredentialResolutionError(t *testing.T) {
	stub := &stubTargetProber{err: errors.New("BAD_ENDPOINT: credential reference could not be resolved")}
	rec := postTargetProbe(t, NewTargetProbeHandler(stub), workspaceapi.StandardConnection("openai", "", "file:/missing/key"), "responses")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "credential file could not be resolved") {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}
