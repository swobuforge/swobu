package connectioncheck

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"

	domain "github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
)

func modelConnection(t *testing.T, providerID, endpoint, ref string) domain.Connection {
	t.Helper()
	p, err := routing.ParseProvider(providerID, profile.SupportsSpec)
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := routing.NewStandardConnection(p, endpoint, ref)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := domain.NewModelSpec(upstream)
	if err != nil {
		t.Fatal(err)
	}
	id, err := connectionidentity.New()
	if err != nil {
		t.Fatal(err)
	}
	value, err := domain.New(id, "Models", spec, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestModelsCheckUsesProviderDiscoveryAndExactScope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer synthetic" {
			t.Error("wrong provider request")
		}
		w.Write([]byte(`{"data":[{"id":"synthetic-model"}]}`))
	}))
	defer server.Close()
	value := modelConnection(t, "openai", server.URL+"/v1", "env:SYNTHETIC")
	resolved := 0
	check, err := checkModels(t.Context(), value, func(ctx context.Context, scope credentialref.Scope, ref string) (string, error) {
		resolved++
		if scope.Kind() != credentialref.ScopeConnection || scope.Identifier() != value.ID().String() || ref != "env:SYNTHETIC" {
			t.Error("wrong credential scope")
		}
		if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 15*time.Second {
			t.Error("unbounded check")
		}
		return "synthetic", nil
	}, server.Client().Transport)
	if err != nil || check.ErrorCode() != domain.CheckOK || check.Authentication() != domain.AuthenticationAccepted || len(check.Capabilities()) != 1 || resolved < 2 {
		t.Fatalf("check=%v err=%v", check, err)
	}
}

func TestModelsCheckDoesNotPromoteAdvisoryOrInvalidSuccess(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		failure    domain.CheckError
		auth       domain.Authentication
	}{
		{"empty", `{"data":[]}`, 200, domain.CheckOK, domain.AuthenticationAccepted},
		{"absent", `{}`, 200, domain.CheckOK, domain.AuthenticationAccepted},
		{"malformed", `private upstream text`, 200, domain.CheckInvalidResponse, domain.AuthenticationUnknown},
		{"missing-catalog", `{}`, 404, domain.CheckUnavailable, domain.AuthenticationUnknown},
		{"denied", `private auth text`, 401, domain.CheckAuthenticationFailed, domain.AuthenticationRejected},
		{"forbidden", `{}`, 403, domain.CheckAuthenticationFailed, domain.AuthenticationRejected},
		{"oversized", strings.Repeat("x", modelCheckBodyLimit+1), 200, domain.CheckInvalidResponse, domain.AuthenticationUnknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(test.status); w.Write([]byte(test.body)) }))
			defer server.Close()
			check, err := checkModels(t.Context(), modelConnection(t, "openai", server.URL, "env:SYNTHETIC"), func(context.Context, credentialref.Scope, string) (string, error) { return "synthetic", nil }, server.Client().Transport)
			if err != nil || check.ErrorCode() != test.failure || check.Authentication() != test.auth || len(check.Capabilities()) != 0 {
				t.Fatal(check, err)
			}
		})
	}
}

func TestModelsCheckMissingCredentialAndPrivateDestination(t *testing.T) {
	value := modelConnection(t, "openai", "https://127.0.0.1:1", "env:SYNTHETIC")
	check, err := Models(t.Context(), value, nil)
	if err != nil || check.ErrorCode() != domain.CheckCredentialUnavailable {
		t.Fatal(check, err)
	}
	t.Setenv("NO_PROXY", "*")
	check, err = Models(t.Context(), value, func(context.Context, credentialref.Scope, string) (string, error) { return "synthetic", nil })
	if err != nil || check.ErrorCode() != domain.CheckNetworkDenied {
		t.Fatal(check, err)
	}
	if publicModelResolution([]netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")}) {
		t.Fatal("mixed DNS admitted")
	}
}

type checkRoundTrip func(*http.Request) (*http.Response, error)

func (f checkRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestModelsCheckBoundsDecodedBodiesPagesAndOrigin(t *testing.T) {
	var encoded bytes.Buffer
	writer := gzip.NewWriter(&encoded)
	writer.Write(bytes.Repeat([]byte("x"), modelCheckBodyLimit+1))
	writer.Close()
	transport := &modelResponseTransport{next: checkRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Encoding": []string{"gzip"}}, Body: io.NopCloser(bytes.NewReader(encoded.Bytes()))}, nil
	})}
	req, _ := http.NewRequestWithContext(t.Context(), "GET", "https://example.test/models", nil)
	if _, err := transport.RoundTrip(req); err == nil || !transport.invalid {
		t.Fatal("decoded bomb admitted")
	}
	count := 0
	transport = &modelResponseTransport{pinOrigin: true, next: checkRoundTrip(func(*http.Request) (*http.Response, error) {
		count++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})}
	for i := 0; i < modelCheckRequestLimit; i++ {
		resp, err := transport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if _, err := transport.RoundTrip(req); err == nil || count != modelCheckRequestLimit {
		t.Fatal("unbounded pages")
	}
	transport.requests = 0
	other, _ := http.NewRequestWithContext(t.Context(), "GET", "https://other.test/models", nil)
	if _, err := transport.RoundTrip(other); !errors.Is(err, errModelNetworkDenied) {
		t.Fatal("page changed credential origin", err)
	}
}

func TestModelsCheckRedirectAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/redirect") {
			w.Header().Set("Location", "/private")
			w.WriteHeader(302)
			return
		}
		if r.URL.Path == "/private" {
			t.Error("followed redirect")
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	resolve := func(context.Context, credentialref.Scope, string) (string, error) { return "synthetic", nil }
	check, err := checkModels(t.Context(), modelConnection(t, "openai", server.URL+"/redirect", "env:SYNTHETIC"), resolve, server.Client().Transport)
	if err != nil || check.ErrorCode() != domain.CheckUnavailable {
		t.Fatal(check, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	check, err = checkModels(ctx, modelConnection(t, "openai", server.URL+"/stall", "env:SYNTHETIC"), resolve, server.Client().Transport)
	if err != nil || check.ErrorCode() != domain.CheckTimeout {
		t.Fatal(check, err)
	}
}

func TestModelsCheckChatGPTRejectionDoesNotRefresh(t *testing.T) {
	requests := 0
	transport := checkRoundTrip(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Method != "GET" || !strings.HasSuffix(r.URL.Path, "/models") {
			t.Error("check attempted refresh or inference")
		}
		return &http.Response{StatusCode: 401, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`private rejection`))}, nil
	})
	check, err := checkModels(t.Context(), modelConnection(t, "chatgpt", "https://chatgpt.com/backend-api/codex", "env:SYNTHETIC"), func(context.Context, credentialref.Scope, string) (string, error) { return "synthetic", nil }, transport)
	if err != nil || check.Authentication() != domain.AuthenticationRejected || requests != 1 {
		t.Fatal(check, err, requests)
	}
}
