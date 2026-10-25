package bedrock

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
)

type checkCredential struct{}

func (checkCredential) ResolveCredential(context.Context, string, string) (string, error) {
	return "synthetic", nil
}

func TestConnectionCatalogUsesExplicitTokenWithoutIdentityEnrichment(t *testing.T) {
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Header.Get("Authorization") != "Bearer synthetic" || r.URL.Path != "/v1/models" || !strings.Contains(r.URL.Host, "us-east-1") {
			t.Errorf("unexpected catalog request path=%s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"synthetic-model"}]}`))}, nil
	})}
	target := provider.NewBedrockTargetSnapshot("check", "", "env:SYNTHETIC", protocolkind.Responses, "responses", "us-east-1", delivery.BufferedDelivery())
	result, err := CheckConnectionCatalog(t.Context(), target, client, checkCredential{})
	if err != nil || len(result.Options) != 1 || requests != 1 || len(result.Diagnostics) != 0 {
		t.Fatalf("options=%d requests=%d err=%v", len(result.Options), requests, err)
	}
}

func TestConnectionCatalogUsesAmbientSigV4WithoutSTS(t *testing.T) {
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_WEB_IDENTITY_TOKEN_FILE", "")
	t.Setenv("AWS_ROLE_ARN", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "synthetic-access")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "synthetic-secret")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_CONFIG_FILE", t.TempDir()+"/missing")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", t.TempDir()+"/missing")
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") || r.URL.Path != "/v1/models" {
			t.Error("wrong ambient catalog authentication")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"synthetic-model"}]}`))}, nil
	})}
	target := provider.NewBedrockTargetSnapshot("check", "", "", protocolkind.Responses, "responses", "us-east-1", delivery.BufferedDelivery())
	result, err := CheckConnectionCatalog(t.Context(), target, client, nil)
	if err != nil || len(result.Options) != 1 || requests != 1 || len(result.Diagnostics) != 0 {
		t.Fatalf("options=%d requests=%d err=%v", len(result.Options), requests, err)
	}
}

func TestConnectionCatalogBoundsInvalidAmbientProcess(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config")
	if err := os.WriteFile(path, []byte("[default]\ncredential_process = printf invalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"AWS_PROFILE", "AWS_DEFAULT_PROFILE", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_WEB_IDENTITY_TOKEN_FILE", "AWS_ROLE_ARN"} {
		t.Setenv(name, "")
	}
	t.Setenv("AWS_CONFIG_FILE", path)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(root, "missing"))
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("dispatched unsupported credential source")
		return nil, nil
	})}
	target := provider.NewBedrockTargetSnapshot("check", "", "", protocolkind.Responses, "responses", "us-east-1", delivery.BufferedDelivery())
	if _, err := CheckConnectionCatalog(t.Context(), target, client, nil); err == nil {
		t.Fatal("accepted unbounded process")
	}
}
