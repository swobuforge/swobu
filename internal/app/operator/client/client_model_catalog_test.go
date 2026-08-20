package operatorclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
)

func TestClientProbeTargetEncodesTypedConnectionBody(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/_swobu/target-probe" {
			t.Fatalf("request method/path = %s %s", r.Method, r.URL.Path)
		}
		var input struct {
			Connection       workspaceapi.Connection `json:"connection"`
			ProviderProtocol string                  `json:"provider_protocol"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		connection, err := input.Connection.RoutingConnection()
		if err != nil || input.ProviderProtocol != "responses" {
			t.Fatalf("probe input = %#v", input)
		}
		if got := string(connection.Provider()); got != "openai" {
			t.Fatalf("probe provider = %q, want openai", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"deployments":[
				{"name":"gpt-4.1","model_name":"gpt-4.1","model_publisher":"openai","model_version":"2024-01","family":"gpt","supported_provider_protocols":["responses","chat_completions"],"default_provider_protocol":"responses"},
				{"name":"gpt-4o","model_name":"gpt-4o","model_publisher":"openai","family":"gpt","default_provider_protocol":"responses"}
			],
			"resolved_provider_protocol":"responses"
		}`))
	}))
	defer server.Close()

	c := New(server.Client(), server.URL)
	result, err := c.ProbeTarget(context.Background(), workspaceapi.StandardConnection("openai", "", "env:OPENAI_API_KEY"), "responses")
	if err != nil {
		t.Fatalf("ProbeTarget returned error: %v", err)
	}
	if len(result.Options) != 2 {
		t.Fatalf("model-authoring options = %d, want 2", len(result.Options))
	}
	d0 := result.Options[0]
	if d0.Name != "gpt-4.1" || d0.ModelName != "gpt-4.1" || d0.ModelPublisher != "openai" || d0.ModelVersion != "2024-01" || d0.Family != "gpt" || d0.DefaultProviderProtocol != "responses" {
		t.Fatalf("first model-authoring option = %#v", d0)
	}
	if len(d0.SupportedProviderProtocols) != 2 || d0.SupportedProviderProtocols[0] != "responses" || d0.SupportedProviderProtocols[1] != "chat_completions" {
		t.Fatalf("first model-authoring option protocols = %v", d0.SupportedProviderProtocols)
	}
	d1 := result.Options[1]
	if d1.Name != "gpt-4o" || d1.ModelName != "gpt-4o" {
		t.Fatalf("second model-authoring option = %#v", d1)
	}
	if result.ResolvedProviderProtocol != "responses" {
		t.Fatalf("resolved_provider_protocol = %q, want responses", result.ResolvedProviderProtocol)
	}
}

func TestClientProbeTarget404ReturnsError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"NOT_FOUND","message":"unknown provider"}}`))
	}))
	defer server.Close()

	c := New(server.Client(), server.URL)
	_, err := c.ProbeTarget(context.Background(), workspaceapi.StandardConnection("openai", "", "env:OPENAI_API_KEY"), "")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "unknown provider") {
		t.Fatalf("error = %q, want 'unknown provider'", err.Error())
	}
}

func TestClientProbeTarget500ReturnsError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"INTERNAL","message":"backend unavailable"}}`))
	}))
	defer server.Close()

	c := New(server.Client(), server.URL)
	_, err := c.ProbeTarget(context.Background(), workspaceapi.StandardConnection("openai", "", "env:OPENAI_API_KEY"), "")
	if err == nil {
		t.Fatal("expected error for 500")
	}
	if !strings.Contains(err.Error(), "backend unavailable") {
		t.Fatalf("error = %q, want 'backend unavailable'", err.Error())
	}
}

// TestClientWorkspaceCommandDecodesFlatCommandError guards the boundary error
// contract: workspace and credential commands marshal the typed
// workspaces.CommandError flat as {"code","message"} (not the nested
// {"error":{...}} shape the ChatGPT login flow emits). A flat body must surface
// its real cause and code, instead of collapsing to a status-code fallback.
func TestClientWorkspaceCommandDecodesFlatCommandError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"INVALID_ARGUMENT","message":"target.protocol: provider protocol is derived and must be omitted"}`))
	}))
	defer server.Close()

	c := New(server.Client(), server.URL)
	_, err := c.ReplaceRoute(context.Background(), workspaceapi.ReplaceRoute{Workspace: "personal", Route: "chatgpt-rodion", Spec: workspaceapi.RouteSpec{}})
	if err == nil {
		t.Fatal("expected error for flat CommandError 400")
	}
	if !strings.Contains(err.Error(), "provider protocol is derived and must be omitted") {
		t.Fatalf("error = %q, want the daemon's real cause", err.Error())
	}
	if strings.Contains(err.Error(), "returned status 400") {
		t.Fatalf("error collapsed to status fallback, real cause lost: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "code=INVALID_ARGUMENT") {
		t.Fatalf("error = %q, want code=INVALID_ARGUMENT surfaced", err.Error())
	}
}
