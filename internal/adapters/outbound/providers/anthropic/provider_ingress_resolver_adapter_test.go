package anthropic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
	"github.com/swobuforge/swobu/internal/exchange"
)

type staticCredentialProvider struct {
	token string
}

func (p staticCredentialProvider) ResolveCredential(context.Context, string, string) (string, error) {
	return p.token, nil
}

func TestResolveProviderIngress_UsesContractDeliveryForStreamingRequests(t *testing.T) {
	t.Parallel()

	sawBody := make(chan string, 1)
	sawErr := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			sawErr <- err
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_ = r.Body.Close()
		sawBody <- string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	adapter := NewExecutor(server.Client(), staticCredentialProvider{token: "test-token"})
	req := exchange.NewProviderRequest(
		"ex-1",
		"responses",
		canonical.NewCanonicalRequest(canonical.RequestParams{
			Model: "claude-sonnet-4-20250514",
			Items: []canonical.CanonicalItem{
				canonical.NewTextItem(canonical.ItemAuthorUser, "ping"),
			},
			Tools: []canonical.ToolDecl{
				canonical.NewFunctionToolDecl("tool_0", "Read", "read files", canonical.NewToolSchemaObject(`{"type":"object","properties":{"path":{"type":"string"}}}`)),
			},
		}),
		carrier.NewWireDocument("", "", "application/json", nil, []byte(`{}`), carrier.Meta{}),
		exchange.NewExecutionContractForDeliveries(
			protocolsurface.StreamingDelivery(protocolsurface.FramingSSE),
			protocolsurface.StreamingDelivery(protocolsurface.FramingSSE),
		),
		exchange.NewRoutableTarget(
			"backend-a",
			"anthropic",
			server.URL,
			"env:ANTHROPIC_API_KEY",
			protocolkind.Messages,
			"credential_ref",
			"",
			"messages",
		),
	)

	ingress, err := adapter.ResolveProviderIngress(context.Background(), req)
	if err != nil {
		t.Fatalf("ResolveProviderIngress returned error: %v", err)
	}

	stream, ok := ingress.(carrier.WireStream)
	if !ok {
		t.Fatalf("ResolveProviderIngress returned %T, want carrier.WireStream", ingress)
	}
	defer func() { _ = stream.Frames.Close() }()

	var body string
	select {
	case err := <-sawErr:
		t.Fatalf("read request body: %v", err)
	case body = <-sawBody:
	}

	if !strings.Contains(body, `"stream":true`) {
		t.Fatalf("request body = %s, want stream=true for streaming contract", body)
	}
	if strings.Contains(body, `"stream":false`) {
		t.Fatalf("request body = %s, want streaming request body, not stream=false", body)
	}
	if !strings.Contains(body, `"tools":[`) {
		t.Fatalf("request body = %s, want tools surface preserved", body)
	}
}
