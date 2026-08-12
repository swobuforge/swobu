package openaifamily

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

func TestLMStudioNativeModelsURL(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		baseURL string
		want    string
		wantErr bool
	}{
		{name: "default", baseURL: "http://127.0.0.1:1234/v1", want: "http://127.0.0.1:1234/api/v1/models"},
		{name: "reverse proxy prefix", baseURL: "https://models.example/lmstudio/v1/", want: "https://models.example/lmstudio/api/v1/models"},
		{name: "reject base without terminal v1", baseURL: "https://models.example/lmstudio", wantErr: true},
		{name: "reject query", baseURL: "https://models.example/v1?tenant=a", wantErr: true},
		{name: "reject fragment", baseURL: "https://models.example/v1#catalog", wantErr: true},
		{name: "reject relative", baseURL: "/v1", wantErr: true},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := LMStudioNativeModelsURL(test.baseURL)
			if test.wantErr {
				if err == nil {
					t.Fatalf("LMStudioNativeModelsURL(%q) = %q, want error", test.baseURL, got)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("LMStudioNativeModelsURL(%q) = %q, %v; want %q", test.baseURL, got, err, test.want)
			}
		})
	}
}

func TestListDeploymentsLMStudioMapsOnlyGenerativeNativeModels(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxy/api/v1/models" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
			"models": [
				{"type":"llm","key":"z/model","display_name":"Zed","publisher":"Zeta","architecture":"qwen","quantization":"Q4","format":"gguf","capabilities":{"vision":true}},
				{"type":"embedding","key":"embed/model","display_name":"Embed"},
				{"type":"llm","key":"a/model","display_name":"Alpha","publisher":"Acme","architecture":"llama","unknown":"ignored"},
				{"type":"llm","key":""}
			]
		}`))
	}))
	defer srv.Close()

	exec := NewExecutor(srv.Client(), stubCredentialResolver{}, LMStudioPolicy())
	got, err := exec.ListDeployments(context.Background(), lmStudioTarget(srv.URL+"/proxy/v1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("deployments = %#v", got)
	}
	if got[0].Name != "a/model" || got[0].ModelName != "Alpha" || got[0].ModelPublisher != "Acme" || got[0].Family != "llama" || got[0].ModelVersion != "" {
		t.Fatalf("first deployment = %#v", got[0])
	}
	if len(got[0].SupportedProviderProtocols) != 0 || got[0].DefaultProviderProtocol != "" {
		t.Fatalf("native model metadata invented protocol support: %#v", got[0])
	}
	wantProtocols := []string{"responses", "responses_stream", "chat_completions", "chat_completions_stream", "messages", "messages_stream"}
	resolved := profile.ResolveProviderDeployment("lmstudio", got[0])
	if !reflect.DeepEqual(resolved.ProtocolOptions(), wantProtocols) {
		t.Fatalf("resolved provider protocols = %#v, want %#v", resolved.ProtocolOptions(), wantProtocols)
	}
}

func TestListDeploymentsLMStudioFallsBackOnlyForUnavailableNativeRoute(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusMethodNotAllowed} {
		status := status
		t.Run(http.StatusText(status), func(t *testing.T) {
			var paths []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				if r.URL.Path == "/api/v1/models" {
					w.WriteHeader(status)
					return
				}
				_, _ = w.Write([]byte(`{"data":[{"id":"fallback-model"}]}`))
			}))
			defer srv.Close()

			exec := NewExecutor(srv.Client(), stubCredentialResolver{}, LMStudioPolicy())
			got, err := exec.ListDeployments(context.Background(), lmStudioTarget(srv.URL+"/v1"))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Name != "fallback-model" {
				t.Fatalf("deployments = %#v", got)
			}
			if !reflect.DeepEqual(paths, []string{"/api/v1/models", "/v1/models"}) {
				t.Fatalf("paths = %#v", paths)
			}
		})
	}
}

func TestListDeploymentsLMStudioDoesNotHideNativeFailures(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, body: `{"error":"token required"}`},
		{name: "forbidden", status: http.StatusForbidden, body: `{"error":"forbidden"}`},
		{name: "rate limited", status: http.StatusTooManyRequests, body: `{"error":"slow down"}`},
		{name: "server error", status: http.StatusInternalServerError, body: `{"error":"broken"}`},
		{name: "malformed success", status: http.StatusOK, body: `{`},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			hits := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits++
				if r.URL.Path != "/api/v1/models" {
					t.Fatalf("unexpected fallback path %q", r.URL.Path)
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer srv.Close()

			exec := NewExecutor(srv.Client(), stubCredentialResolver{}, LMStudioPolicy())
			if _, err := exec.ListDeployments(context.Background(), lmStudioTarget(srv.URL+"/v1")); err == nil {
				t.Fatal("expected native catalog failure")
			}
			if hits != 1 {
				t.Fatalf("requests = %d, want 1", hits)
			}
		})
	}
}

func TestListDeploymentsRejectsProviderPolicyIdentityDrift(t *testing.T) {
	t.Parallel()
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer srv.Close()

	exec := NewExecutor(srv.Client(), stubCredentialResolver{}, LMStudioPolicy())
	target := provider.NewCustomTargetSnapshot("draft", srv.URL+"/v1", "", protocolkind.Responses, "", "responses", "Authorization")
	if _, err := exec.ListDeployments(context.Background(), target); err == nil {
		t.Fatal("expected exact provider policy mismatch")
	}
	if hits != 0 {
		t.Fatalf("upstream hits = %d, want 0", hits)
	}
}

func lmStudioTarget(baseURL string) provider.TargetSnapshot {
	return provider.NewTargetSnapshot("draft", "lmstudio", baseURL, "", protocolkind.Responses, "", "responses")
}
