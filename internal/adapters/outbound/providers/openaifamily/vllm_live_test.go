//go:build integration_live

package openaifamily

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	credentialsadapter "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
)

// TestLiveVLLMStandardServing proves the first-class provider against a real
// server without treating one model's tool or reasoning behavior as capability.
func TestLiveVLLMStandardServing(t *testing.T) {
	if strings.TrimSpace(os.Getenv("SWOBU_LIVE_VLLM")) != "1" {
		t.Skip("set SWOBU_LIVE_VLLM=1 with SWOBU_VLLM_MODEL to certify a real vLLM server")
	}
	model := strings.TrimSpace(os.Getenv("SWOBU_VLLM_MODEL"))
	if model == "" {
		t.Fatal("set SWOBU_VLLM_MODEL to the served model identity")
	}
	baseURL := strings.TrimSpace(os.Getenv("SWOBU_VLLM_BASE_URL"))
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8000/v1"
	}
	credentialRef := ""
	if strings.TrimSpace(os.Getenv("VLLM_API_KEY")) != "" {
		credentialRef = "env:VLLM_API_KEY"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	exec := NewExecutor(http.DefaultClient, credentialsadapter.NewResolver(), NewVLLMPolicy())
	discoveryTarget := liveVLLMTarget(baseURL, credentialRef, model, protocolkind.Responses, "responses")
	deployments, err := exec.ListDeployments(ctx, discoveryTarget)
	if err != nil {
		t.Fatalf("live vLLM model discovery: %v", err)
	}
	found := false
	for _, deployment := range deployments {
		if deployment.Name == model {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("served model %q missing from deployments %#v", model, deployments)
	}

	requests := []struct {
		name     string
		kind     protocolkind.ProtocolKind
		protocol string
		body     string
		stream   bool
	}{
		{name: "responses", kind: protocolkind.Responses, protocol: "responses", body: `{"model":"` + model + `","input":"Reply briefly.","max_output_tokens":8,"store":false}`},
		{name: "chat_completions", kind: protocolkind.ChatCompletions, protocol: "chat_completions", body: `{"model":"` + model + `","messages":[{"role":"user","content":"Reply briefly."}],"max_tokens":8}`},
		{name: "messages", kind: protocolkind.Messages, protocol: "messages", body: `{"model":"` + model + `","messages":[{"role":"user","content":"Reply briefly."}],"max_tokens":8}`},
		{name: "responses_stream", kind: protocolkind.Responses, protocol: "responses_stream", body: `{"model":"` + model + `","input":"Reply briefly.","max_output_tokens":8,"stream":true,"store":false}`, stream: true},
	}
	for _, request := range requests {
		t.Run(request.name, func(t *testing.T) {
			target := liveVLLMTarget(baseURL, credentialRef, model, request.kind, request.protocol)
			backend, err := exec.ResolveBackend(target)
			if err != nil {
				t.Fatal(err)
			}
			ingress, err := backend.Transport.Send(ctx, carrier.NewDocument(request.kind, "application/json", nil, []byte(request.body), carrier.Meta{}))
			if err != nil {
				t.Fatalf("live vLLM %s: %v", request.protocol, err)
			}
			switch got := ingress.(type) {
			case provider.DocumentIngress:
				if request.stream || got.Document.IsEmpty() {
					t.Fatalf("%s ingress = %#v", request.protocol, got)
				}
			case provider.StreamIngress:
				if !request.stream {
					_ = got.Stream.Body.Close()
					t.Fatalf("%s unexpectedly streamed", request.protocol)
				}
				buffer := make([]byte, 256)
				n, readErr := got.Stream.Body.Read(buffer)
				closeErr := got.Stream.Body.Close()
				if n == 0 || (readErr != nil && readErr != io.EOF) {
					t.Fatalf("%s first stream read = %d, %v", request.protocol, n, readErr)
				}
				if closeErr != nil {
					t.Fatalf("%s close stream: %v", request.protocol, closeErr)
				}
			default:
				t.Fatalf("%s ingress type = %T", request.protocol, ingress)
			}
		})
	}
	t.Logf("certified vLLM model=%s base=%s credential=%t", model, baseURL, credentialRef != "")
}

func liveVLLMTarget(baseURL, credentialRef, model string, kind protocolkind.ProtocolKind, protocol string) provider.TargetSnapshot {
	target := provider.NewTargetSnapshot("live-vllm", "vllm", baseURL, credentialRef, kind, "", protocol)
	target.Model = model
	return target
}
