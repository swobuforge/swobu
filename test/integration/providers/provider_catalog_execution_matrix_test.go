package providers_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	anthropicadapter "github.com/metrofun/swobu/internal/adapters/outbound/providers/anthropic"
	customadapter "github.com/metrofun/swobu/internal/adapters/outbound/providers/custom"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/internal/domain/providercatalog"
	"github.com/metrofun/swobu/internal/ports"
)

type matrixCredentialResolver struct{}

func (matrixCredentialResolver) ResolveCredential(context.Context, string, string) (string, error) {
	return "token-123", nil
}

type executeAdapter interface {
	Execute(ctx context.Context, req ports.ExecuteRequest) (ports.ExecuteResponse, error)
}

func TestProviderCatalog_DeclaredProtocolsAndModelsAreExecutable(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			_, _ = w.Write([]byte(`{"id":"chatcmpl_1","model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
		case strings.HasSuffix(r.URL.Path, "/responses"):
			_, _ = w.Write([]byte(`{"id":"resp_1","model":"m","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"output_text":"ok"}`))
		case strings.HasSuffix(r.URL.Path, "/completions"):
			_, _ = w.Write([]byte(`{"id":"cmpl_1","model":"m","choices":[{"text":"ok","finish_reason":"stop"}]}`))
		case strings.HasSuffix(r.URL.Path, "/messages"):
			_, _ = w.Write([]byte(`{"id":"msg_1","model":"m","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"unexpected path"}`))
		}
	}))
	defer upstream.Close()

	resolver := matrixCredentialResolver{}
	adapters := map[string]executeAdapter{
		providercatalog.AdapterCustomOpenAICompatible: customadapter.NewExecutor(upstream.Client(), resolver),
		providercatalog.AdapterAnthropicMessages:      anthropicadapter.NewExecutor(upstream.Client(), resolver),
	}

	for _, profile := range providercatalog.All() {
		adapter, ok := adapters[profile.Adapter]
		if !ok {
			t.Fatalf("provider %q references unknown adapter %q", profile.Spec, profile.Adapter)
		}
		for _, protocolKind := range profile.SupportedProtocols {
			for _, model := range []string{"m", "model-explicit-1"} {
				t.Run(profile.Spec+"/"+string(protocolKind)+"/"+model, func(t *testing.T) {
					baseURL := upstream.URL + "/v1"

					req := matrixCanonicalRequestForProtocol(t, protocolKind, model)
					resp, err := adapter.Execute(context.Background(), ports.NewExecuteRequest(
						req, ports.NewExecutionContract(compatibility.DeliveryModeBuffered),
						ports.NewRoutableTarget("backend-a", profile.Spec, baseURL, "cred-1", protocolKind, "", ""),
					))
					if err != nil {
						var compatErr compatibility.Error
						if errors.As(err, &compatErr) {
							if compatErr.Code == compatibility.ErrorCodeUnsupportedOperation || compatErr.Code == compatibility.ErrorCodeUnsupportedDelivery {
								t.Fatalf("declared compatibility-case support is not executable: provider=%q adapter=%q protocol=%q model=%q code=%q message=%q", profile.Spec, profile.Adapter, protocolKind, model, compatErr.Code, compatErr.Message)
							}
						}
						t.Fatalf("execute failed for compatibility_case provider=%q protocol=%q model=%q: %v", profile.Spec, protocolKind, model, err)
					}
					output := resp.Output()
					if gotModel := output.Model(); gotModel == "" {
						t.Fatalf("compatibility_case provider=%q protocol=%q model=%q returned empty output model", profile.Spec, protocolKind, model)
					}
					_ = resp.Close()
				})
			}
		}
	}
}

func matrixCanonicalRequestForProtocol(t *testing.T, protocolKind protocolsurface.Kind, model string) compatibility.CanonicalRequest {
	t.Helper()
	switch protocolKind {
	case protocolsurface.ChatCompletions, protocolsurface.Messages:
		return compatibility.NewDialogRequest(model, []compatibility.CanonicalItem{compatibility.NewTextItem(compatibility.ItemAuthorUser, "hi")})
	case protocolsurface.Responses:
		return compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{Model: model, InputText: "hi"})
	case protocolsurface.Completions:
		return compatibility.NewPromptRequest(model, "hi")
	default:
		t.Fatalf("unsupported protocol in test: %q", protocolKind)
		return nil
	}
}
