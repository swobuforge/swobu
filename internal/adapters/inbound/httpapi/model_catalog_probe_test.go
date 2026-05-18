package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	providersadapter "github.com/swobuforge/swobu/internal/adapters/outbound/providers"
)

type staticCredentialResolver map[string]string

func (r staticCredentialResolver) ResolveCredential(_ context.Context, _, credentialRef string) (string, error) {
	return r[credentialRef], nil
}

func TestModelCatalogProbeHandler_BedrockEnvMode_LoadsModelIDs(t *testing.T) {
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "bedrock-token-123")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openai/v1/models" {
			t.Fatalf("path=%q want /openai/v1/models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer bedrock-token-123" {
			t.Fatalf("Authorization=%q want Bearer bedrock-token-123", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"anthropic.claude-3-5-sonnet"},{"id":"amazon.nova-lite-v1"}]}`))
	}))
	defer upstream.Close()

	bundle := providersadapter.NewProviderServicesBundle(upstream.Client(), staticCredentialResolver{})
	h := NewModelCatalogProbeHandler(bundle.ModelCatalog)

	query := url.Values{}
	query.Set("provider_spec", "bedrock")
	query.Set("base_url", upstream.URL+"/openai/v1")
	query.Set("credential_ref", "env:AWS_BEARER_TOKEN_BEDROCK")
	req := httptest.NewRequest(http.MethodGet, "/_swobu/model-catalog/probe?"+query.Encode(), nil)
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
}

func TestModelCatalogProbeHandler_BedrockProfileMode_RegionMissingReturnsError(t *testing.T) {
	bundle := providersadapter.NewProviderServicesBundle(http.DefaultClient, staticCredentialResolver{})
	h := NewModelCatalogProbeHandler(bundle.ModelCatalog)

	query := url.Values{}
	query.Set("provider_spec", "bedrock")
	query.Set("base_url", "")
	query.Set("credential_ref", "aws_profile")
	req := httptest.NewRequest(http.MethodGet, "/_swobu/model-catalog/probe?"+query.Encode(), nil)
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
	if out.Error == "" {
		t.Fatalf("expected probe error for missing region/base_url")
	}
}
