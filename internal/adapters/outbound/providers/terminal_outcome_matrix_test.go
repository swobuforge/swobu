package providers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/exchange/codecresolver"
	"github.com/swobuforge/swobu/internal/profile"
)

type terminalMatrixCase struct {
	name             string
	providerID       string
	protocolKind     protocolkind.ProtocolKind
	providerProtocol string
	clientFamily     canonical.ClientFamily
	basePath         string
	useAzureLocator  bool
	credentialRef    string
	providerDelivery delivery.Delivery
	responseStatus   int
	responseHeaders  map[string]string
	responseBody     string
	wantPath         string
	wantOutputReason string
	wantWireContains []string
}

func TestProviderIngress_TerminalOutcomeMatrix(t *testing.T) {
	t.Parallel()

	cases := []terminalMatrixCase{
		{
			name:             "openai chat completions content filter",
			providerID:       string(profile.ProviderSpecOpenAI),
			protocolKind:     protocolkind.ChatCompletions,
			providerProtocol: "chat_completions",
			clientFamily:     canonical.ClientFamilyChatCompletions,
			basePath:         "/v1",
			credentialRef:    "cred-1",
			providerDelivery: delivery.BufferedDelivery(),
			responseStatus:   http.StatusOK,
			responseHeaders:  map[string]string{"Content-Type": "application/json"},
			responseBody:     `{"id":"chatcmpl_1","model":"m","choices":[{"message":{"role":"assistant","content":""},"finish_reason":"content_filter","content_filter_result":{"error":{"code":"content_filter","message":"ResponsibleAI result indicated block action."}}}],"usage":{"prompt_tokens":12,"completion_tokens":0}}`,
			wantPath:         "/v1/chat/completions",
			wantOutputReason: "content_filter",
			wantWireContains: []string{`"finish_reason":"content_filter"`},
		},
		{
			name:             "openrouter chat completions content filter",
			providerID:       string(profile.ProviderSpecOpenRouter),
			protocolKind:     protocolkind.ChatCompletions,
			providerProtocol: "chat_completions",
			clientFamily:     canonical.ClientFamilyChatCompletions,
			basePath:         "/v1",
			credentialRef:    "cred-1",
			providerDelivery: delivery.BufferedDelivery(),
			responseStatus:   http.StatusOK,
			responseHeaders:  map[string]string{"Content-Type": "application/json"},
			responseBody:     `{"id":"chatcmpl_1","model":"m","choices":[{"message":{"role":"assistant","content":""},"finish_reason":"content_filter","content_filter_result":{"error":{"code":"content_filter","message":"ResponsibleAI result indicated block action."}}}],"usage":{"prompt_tokens":12,"completion_tokens":0}}`,
			wantPath:         "/v1/chat/completions",
			wantOutputReason: "content_filter",
			wantWireContains: []string{`"finish_reason":"content_filter"`},
		},
		{
			name:             "openai responses incomplete content filter",
			providerID:       string(profile.ProviderSpecOpenAI),
			protocolKind:     protocolkind.Responses,
			providerProtocol: "responses",
			clientFamily:     canonical.ClientFamilyResponses,
			basePath:         "/v1",
			credentialRef:    "cred-1",
			providerDelivery: delivery.BufferedDelivery(),
			responseStatus:   http.StatusOK,
			responseHeaders:  map[string]string{"Content-Type": "application/json"},
			responseBody:     `{"id":"resp_1","model":"m","status":"incomplete","incomplete_details":{"reason":"content_filter"},"content_filters":[{"source_type":"completion","blocked":true}],"output":[]}`,
			wantPath:         "/v1/responses",
			wantOutputReason: "content_filter",
			wantWireContains: []string{`"status":"incomplete"`, `"incomplete_details":{"reason":"content_filter"}`},
		},
		{
			name:             "openaicompat completions content filter",
			providerID:       string(profile.ProviderSpecOpenAICompatible),
			protocolKind:     protocolkind.Completions,
			providerProtocol: "completions",
			clientFamily:     canonical.ClientFamilyCompletions,
			basePath:         "/v1",
			credentialRef:    "cred-1",
			providerDelivery: delivery.BufferedDelivery(),
			responseStatus:   http.StatusOK,
			responseHeaders:  map[string]string{"Content-Type": "application/json"},
			responseBody:     `{"id":"cmpl_1","model":"m","choices":[{"text":"","finish_reason":"content_filter"}],"usage":{"prompt_tokens":12,"completion_tokens":0}}`,
			wantPath:         "/v1/completions",
			wantOutputReason: "content_filter",
			wantWireContains: []string{`"finish_reason":"content_filter"`},
		},
		{
			name:             "ollama completions stop",
			providerID:       string(profile.ProviderSpecOllama),
			protocolKind:     protocolkind.Completions,
			providerProtocol: "completions",
			clientFamily:     canonical.ClientFamilyCompletions,
			basePath:         "/v1",
			credentialRef:    "cred-1",
			providerDelivery: delivery.BufferedDelivery(),
			responseStatus:   http.StatusOK,
			responseHeaders:  map[string]string{"Content-Type": "application/json"},
			responseBody:     `{"id":"cmpl_1","model":"m","choices":[{"text":"ok","finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":2}}`,
			wantPath:         "/v1/completions",
			wantOutputReason: "stop",
			wantWireContains: []string{`"finish_reason":"stop"`},
		},
		{
			name:             "azure responses incomplete content filter",
			providerID:       string(profile.ProviderSpecAzure),
			protocolKind:     protocolkind.Responses,
			providerProtocol: "responses",
			clientFamily:     canonical.ClientFamilyResponses,
			useAzureLocator:  true,
			credentialRef:    "cred-1",
			providerDelivery: delivery.BufferedDelivery(),
			responseStatus:   http.StatusOK,
			responseHeaders:  map[string]string{"Content-Type": "application/json"},
			responseBody:     `{"id":"resp_1","model":"m","status":"incomplete","incomplete_details":{"reason":"content_filter"},"content_filters":[{"source_type":"completion","blocked":true}],"output":[]}`,
			wantPath:         "/openai/v1/responses",
			wantOutputReason: "content_filter",
			wantWireContains: []string{`"status":"incomplete"`, `"incomplete_details":{"reason":"content_filter"}`},
		},
		{
			name:             "anthropic messages refusal",
			providerID:       string(profile.ProviderSpecAnthropic),
			protocolKind:     protocolkind.Messages,
			providerProtocol: "messages",
			clientFamily:     canonical.ClientFamilyMessages,
			basePath:         "/v1",
			credentialRef:    "cred-1",
			providerDelivery: delivery.BufferedDelivery(),
			responseStatus:   http.StatusOK,
			responseHeaders:  map[string]string{"Content-Type": "application/json"},
			responseBody:     `{"id":"msg_1","model":"claude-x","content":[{"type":"text","text":"I can't help with that."}],"stop_reason":"refusal","usage":{"input_tokens":12,"output_tokens":1}}`,
			wantPath:         "/v1/messages",
			wantOutputReason: "refusal",
			wantWireContains: []string{`"stop_reason":"refusal"`},
		},
		{
			name:             "bedrock messages end turn",
			providerID:       string(profile.ProviderSpecBedrock),
			protocolKind:     protocolkind.Messages,
			providerProtocol: "messages",
			clientFamily:     canonical.ClientFamilyMessages,
			basePath:         "/v1",
			credentialRef:    "env:AWS_BEARER_TOKEN_BEDROCK",
			providerDelivery: delivery.BufferedDelivery(),
			responseStatus:   http.StatusOK,
			responseHeaders:  map[string]string{"Content-Type": "application/json"},
			responseBody:     `{"id":"msg_1","model":"openai.gpt-4.1-mini","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`,
			wantPath:         "/v1/messages",
			wantOutputReason: "end_turn",
			wantWireContains: []string{`"stop_reason":"end_turn"`},
		},
		{
			name:             "chatgpt responses stream content filter",
			providerID:       string(profile.ProviderSpecChatGPT),
			protocolKind:     protocolkind.Responses,
			providerProtocol: "responses_stream",
			clientFamily:     canonical.ClientFamilyResponses,
			basePath:         "/v1",
			credentialRef:    "cred-1",
			providerDelivery: delivery.StreamingDelivery(delivery.FramingSSE),
			responseStatus:   http.StatusOK,
			responseHeaders:  map[string]string{"Content-Type": "text/event-stream"},
			responseBody: "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
				"event: response.incomplete\ndata: {\"type\":\"response.incomplete\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"incomplete\",\"incomplete_details\":{\"reason\":\"content_filter\"},\"content_filters\":[{\"source_type\":\"completion\",\"blocked\":true}],\"output\":[]}}\n\n",
			wantPath:         "/v1/responses",
			wantOutputReason: "content_filter",
			wantWireContains: []string{`"type":"response.incomplete"`, `"reason":"content_filter"`},
		},
	}

	resolver := codecresolver.NewRuntimeCodecResolver()
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Fatalf("request method = %s want POST", r.Method)
				}
				if r.URL.Path != tc.wantPath {
					t.Fatalf("request path = %s want %s", r.URL.Path, tc.wantPath)
				}
				for key, value := range tc.responseHeaders {
					w.Header().Set(key, value)
				}
				w.WriteHeader(tc.responseStatus)
				_, _ = io.WriteString(w, tc.responseBody)
			}))
			defer srv.Close()

			client := rewritingClientForServer(t, srv)
			registry := NewProviderRegistry(client, testCredentialResolver{}, "")
			request := canonical.NewCanonicalRequest(canonical.RequestParams{
				Model: "m",
				Items: []canonical.CanonicalItem{
					canonical.NewTextItem(canonical.ItemAuthorUser, "hi"),
				},
			})
			wireRequestResult, err := resolver.ProviderRequestDocumentEncoder(tc.protocolKind).EncodeProviderRequestDocument(request, tc.providerDelivery, "ex_matrix")
			if err != nil {
				t.Fatalf("EncodeProviderRequestDocument returned error: %v", err)
			}
			req := exchange.NewProviderRequest(
				"ex_matrix",
				clientFamilyForProtocol(tc.protocolKind),
				request,
				wireRequestResult.Value,
				exchange.NewExecutionContract(tc.providerDelivery),
				exchange.NewRoutableTarget("backend-a", tc.providerID, targetBaseURLForCase(srv.URL, tc), tc.credentialRef, tc.protocolKind, "credential_ref", "", tc.providerProtocol),
			)

			ingress, err := registry.ResolveProviderIngress(context.Background(), req)
			if err != nil {
				t.Fatalf("ResolveProviderIngress returned error: %v", err)
			}

			switch tc.providerDelivery.Mode {
			case delivery.Buffered:
				doc, ok := ingress.(carrier.CarrierDocument)
				if !ok {
					t.Fatalf("ResolveProviderIngress returned %T, want carrier.CarrierDocument", ingress)
				}
				readerResult, err := resolver.ProviderDocumentDecoder(tc.protocolKind, tc.providerDelivery).DecodeProviderDocument(context.Background(), doc, req.ExchangeID)
				if err != nil {
					t.Fatalf("DecodeProviderDocument returned error: %v", err)
				}
				closed, err := canonical.ReadClosedEnvelope(context.Background(), readerResult.Value, canonical.EnvResponse)
				if err != nil {
					t.Fatalf("ReadClosedEnvelope returned error: %v", err)
				}
				out, err := closed.ProjectResponse()
				if err != nil {
					t.Fatalf("ProjectResponse returned error: %v", err)
				}
				if got := out.FinishReason(); got != tc.wantOutputReason {
					t.Fatalf("finish reason = %q, want %q", got, tc.wantOutputReason)
				}
				clientResult, err := resolver.ClientCodec(clientFamilyForProtocol(tc.protocolKind)).EncodeResponseDocument(out)
				if err != nil {
					t.Fatalf("EncodeResponseDocument returned error: %v", err)
				}
				body := string(clientResult.Value.RawBytes())
				for _, want := range tc.wantWireContains {
					if !strings.Contains(body, want) {
						t.Fatalf("encoded body missing %q: %s", want, body)
					}
				}
			case delivery.Streaming:
				stream, ok := ingress.(carrier.CarrierStream)
				if !ok {
					t.Fatalf("ResolveProviderIngress returned %T, want carrier.CarrierStream", ingress)
				}
				readerResult, err := resolver.ProviderEnvelopeDecoder(tc.protocolKind, tc.providerDelivery).DecodeProviderEnvelope(stream, req.ExchangeID)
				if err != nil {
					t.Fatalf("DecodeProviderEnvelope returned error: %v", err)
				}
				closed, err := canonical.ReadClosedEnvelope(context.Background(), readerResult.Value, canonical.EnvResponse)
				if err != nil {
					t.Fatalf("ReadClosedEnvelope returned error: %v", err)
				}
				out, err := closed.ProjectResponse()
				if err != nil {
					t.Fatalf("ProjectResponse returned error: %v", err)
				}
				if got := out.FinishReason(); got != tc.wantOutputReason {
					t.Fatalf("finish reason = %q, want %q", got, tc.wantOutputReason)
				}
				events := canonical.SynthesizeResponseEnvelopeEvents(req.ExchangeID, out.ResultID(), out.Model(), out.Items(), out.FinishReason(), out.Usage())
				clientResult, err := resolver.ClientCodec(clientFamilyForProtocol(tc.protocolKind)).EncodeResponseStream(canonical.NewSliceEventReader(events), tc.providerDelivery)
				if err != nil {
					t.Fatalf("EncodeResponseStream returned error: %v", err)
				}
				raw, err := io.ReadAll(carrier.ReadCloserFromFrameReader(clientResult.Value.Frames))
				if err != nil {
					t.Fatalf("ReadAll returned error: %v", err)
				}
				body := string(raw)
				for _, want := range tc.wantWireContains {
					if !strings.Contains(body, want) {
						t.Fatalf("encoded stream missing %q: %s", want, body)
					}
				}
			default:
				t.Fatalf("unsupported delivery mode %v", tc.providerDelivery.Mode)
			}
		})
	}
}

func TestProviderIngress_AzurePromptContentFilterReturnsBackendError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openai/v1/responses" {
			t.Fatalf("request path = %s want /openai/v1/responses", r.URL.Path)
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"prompt blocked","code":"content_filter"}}`)
	}))
	defer srv.Close()

	client := rewritingClientForServer(t, srv)
	registry := NewProviderRegistry(client, testCredentialResolver{}, "")
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "m",
		Items: []canonical.CanonicalItem{canonical.NewTextItem(canonical.ItemAuthorUser, "hi")},
	})
	resolver := codecresolver.NewRuntimeCodecResolver()
	wireRequestResult, err := resolver.ProviderRequestDocumentEncoder(protocolkind.Responses).EncodeProviderRequestDocument(request, delivery.BufferedDelivery(), "ex_prompt")
	if err != nil {
		t.Fatalf("EncodeProviderRequestDocument returned error: %v", err)
	}
	req := exchange.NewProviderRequest(
		"ex_prompt",
		canonical.ClientFamilyResponses,
		request,
		wireRequestResult.Value,
		exchange.NewExecutionContract(delivery.BufferedDelivery()),
		exchange.NewRoutableTarget("backend-a", string(profile.ProviderSpecAzure), "contact-8837-resource", "cred-1", protocolkind.Responses, "credential_ref", "", "responses"),
	)

	_, err = registry.ResolveProviderIngress(context.Background(), req)
	if err == nil {
		t.Fatal("ResolveProviderIngress returned nil error, want backend error")
	}
	if !strings.Contains(err.Error(), "prompt blocked") {
		t.Fatalf("ResolveProviderIngress error = %v, want prompt blocked detail", err)
	}
}

func clientFamilyForProtocol(kind protocolkind.ProtocolKind) canonical.ClientFamily {
	switch kind {
	case protocolkind.ChatCompletions:
		return canonical.ClientFamilyChatCompletions
	case protocolkind.Responses:
		return canonical.ClientFamilyResponses
	case protocolkind.Completions:
		return canonical.ClientFamilyCompletions
	case protocolkind.Messages:
		return canonical.ClientFamilyMessages
	default:
		return ""
	}
}

func targetBaseURLForCase(serverURL string, tc terminalMatrixCase) string {
	if tc.useAzureLocator {
		return "contact-8837-resource"
	}
	return strings.TrimRight(serverURL, "/") + tc.basePath
}

type rewriteRoundTripper struct {
	base   http.RoundTripper
	target *url.URL
}

func (rt rewriteRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = rt.target.Scheme
	clone.URL.Host = rt.target.Host
	clone.Host = rt.target.Host
	if rt.base == nil {
		rt.base = http.DefaultTransport
	}
	return rt.base.RoundTrip(clone)
}

func rewritingClientForServer(t *testing.T, srv *httptest.Server) *http.Client {
	t.Helper()
	target, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	return &http.Client{Transport: rewriteRoundTripper{base: srv.Client().Transport, target: target}}
}
