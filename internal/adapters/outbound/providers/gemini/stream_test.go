package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestInteractionsStreamProjectsTextLifecycleAndCumulativeUsage(t *testing.T) {
	stream := decodeGeminiStream(t, strings.Join([]string{
		`sse: ignored`,
		`data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}`,
		``,
		`data: {"event_type":"step.start","index":0,"step":{"type":"model_output"}}`,
		``,
		`data: {"event_type":"step.delta","index":0,"delta":{"type":"text","text":"hello "}}`,
		``,
		`data: {"event_type":"step.delta","index":0,"delta":{"type":"text","text":"world"},"metadata":{"total_usage":{"total_input_tokens":3}}}`,
		``,
		`data: {"event_type":"step.stop","index":0,"usage":{"total_output_tokens":2,"total_thought_tokens":5,"total_cached_tokens":1}}`,
		``,
		`data: {"event_type":"interaction.completed","interaction":{"id":"interaction_1","status":"completed"}}`,
		``,
	}, "\n"))

	bound := canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{
		SwobuID: "resp_gemini", TargetID: "gemini-target", TargetVersion: 4,
	})
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(bound), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	interactions := response.Response().Interactions
	if interactions == nil || interactions.ProviderInteractionID != "interaction_1" || interactions.TargetID != "gemini-target" || interactions.TargetVersion != 4 {
		t.Fatalf("bound Gemini continuation = %#v", response.Response())
	}
	if response.Response().SwobuID != "resp_gemini" || response.Model() != "gemini-model" || response.Completion().Class() != canonical.CompletionCompleted {
		t.Fatalf("response = %#v", response)
	}
	items := response.Items()
	if len(items) != 1 {
		t.Fatalf("items = %#v", items)
	}
	message, ok := items[0].Message()
	if !ok || message.Role() != canonical.MessageRoleAssistant || len(message.Content()) != 1 {
		t.Fatalf("message = %#v", message)
	}
	text, _ := message.Content()[0].Text()
	if text.Text() != "hello world" {
		t.Fatalf("text = %q", text.Text())
	}
	usage := response.Usage()
	assertUsage(t, usage, 3, 2, 5, 1)
}

func TestInteractionsStreamProjectsThoughtSummaryAndExactFunctionRouting(t *testing.T) {
	functionKey := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "namespace/lookup")
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gemini-model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, canonicaltest.MustFunctionTool(functionKey, "", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]())),
		},
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	providerName, err := names.WireName(functionKey)
	if err != nil {
		t.Fatal(err)
	}
	if providerName == functionKey.Name() {
		t.Fatalf("attempt tool name = %q, want allocated namespace-safe alias", providerName)
	}
	raw := `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"step.start","index":0,"step":{"type":"thought","summary":[{"type":"text","text":"checking "}]}}

data: {"event_type":"step.delta","index":0,"delta":{"type":"thought_summary","content":{"type":"text","text":"sources"}}}

data: {"event_type":"step.stop","index":0}

data: {"event_type":"step.start","index":1,"step":{"type":"function_call","id":"call_lookup","name":"%s","arguments":{"q":"x"}}}

data: {"event_type":"step.delta","index":1,"delta":{"type":"function_call","id":"call_lookup","name":"%s","arguments":{"q":"x"}}}

data: {"event_type":"step.stop","index":1}

data: {"event_type":"interaction.completed","interaction":{"id":"interaction_1","status":"requires_action"}}

`
	stream := decodeGeminiStreamForProviderRequest(t, provider.Request{ExchangeID: "gemini-exchange", Canonical: request, ToolNames: names, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)}, fmt.Sprintf(raw, providerName, providerName))
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "resp_thought_function"})), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	if response.Response().Interactions == nil || response.Response().Interactions.ProviderInteractionID.String() != "interaction_1" {
		t.Fatalf("response ref = %#v, want exact stateful Interactions continuation", response.Response())
	}
	items := response.Items()
	if len(items) != 2 {
		t.Fatalf("items = %#v", items)
	}
	reasoning, ok := items[0].Reasoning()
	if !ok || len(reasoning.Parts()) != 1 || reasoning.Parts()[0].Text() != "checking sources" {
		t.Fatalf("reasoning = %#v", items[0])
	}
	call, ok := items[1].ToolCall()
	if !ok || call.CallID().String() != "call_lookup" || call.Tool() != functionKey {
		t.Fatalf("call = %#v", items[1])
	}
	object, ok := call.Input().Object()
	if !ok || object.String() != `{"q":"x"}` {
		t.Fatalf("arguments = %#v", call.Input())
	}
}

func TestInteractionsStreamRetainsSignatureOnlyThoughtAsOpaqueReplay(t *testing.T) {
	raw := `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"step.start","index":0,"step":{"type":"thought"}}

data: {"event_type":"step.delta","index":0,"delta":{"type":"thought_signature","signature":"opaque-signature"}}

data: {"event_type":"step.stop","index":0}

data: {"event_type":"step.start","index":1,"step":{"type":"model_output"}}

data: {"event_type":"step.delta","index":1,"delta":{"type":"text","text":"done"}}

data: {"event_type":"step.stop","index":1}

data: {"event_type":"interaction.completed","interaction":{"id":"interaction_1","status":"completed"}}

`
	stream := decodeGeminiStream(t, raw)
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "resp_signature_only"})), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	if response.Response().Interactions == nil || response.Response().Interactions.ProviderInteractionID.String() != "interaction_1" {
		t.Fatalf("response ref = %#v, want exact stateful Interactions continuation", response.Response())
	}
	items := response.Items()
	if len(items) != 2 {
		t.Fatalf("items = %#v, want opaque thought and readable model output", items)
	}
	reasoning, ok := items[0].Reasoning()
	if !ok || len(reasoning.Parts()) != 0 {
		t.Fatalf("item = %#v, want opaque-only reasoning", items[0])
	}
	if raw, exact := reasoning.Opaque().Interactions(); !exact || !strings.Contains(string(raw), `"signature":"opaque-signature"`) {
		t.Fatalf("opaque replay = %s/%t", raw, exact)
	}
	message, ok := items[1].Message()
	if !ok {
		t.Fatalf("item = %#v, want message", items[1])
	}
	text, _ := message.Content()[0].Text()
	if text.Text() != "done" {
		t.Fatalf("text = %q", text.Text())
	}
	if got := stream.Changes(); len(got) != 0 {
		t.Fatalf("changes = %#v, want exact replay capture", got)
	}
}

func TestInteractionsStreamRejectsUnknownOrContradictoryFunctionCall(t *testing.T) {
	functionKey := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gemini-model"),
		Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, canonicaltest.MustFunctionTool(functionKey, "", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]()))},
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{
		"unknown name": `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"step.start","index":0,"step":{"type":"function_call","id":"call_lookup","name":"other","arguments":{}}}

`,
		"contradictory delta": `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"step.start","index":0,"step":{"type":"function_call","id":"call_lookup","name":"lookup","arguments":{}}}

data: {"event_type":"step.delta","index":0,"delta":{"type":"function_call","id":"call_other","name":"lookup","arguments":{}}}

`,
	} {
		t.Run(name, func(t *testing.T) {
			stream := decodeGeminiStreamForProviderRequest(t, provider.Request{ExchangeID: "gemini-exchange", Canonical: request, ToolNames: names, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)}, raw)
			for {
				_, err := stream.Next(context.Background())
				if err == nil {
					continue
				}
				var backend canonical.BackendError
				if !errors.As(err, &backend) {
					t.Fatalf("error = %T %v, want backend failure", err, err)
				}
				return
			}
		})
	}
}

func TestInteractionsStreamAccumulatesIncrementalFunctionArguments(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	request := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("gemini-model"), Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, canonicaltest.MustFunctionTool(key, "", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]()))}})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	raw := `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"step.start","index":0,"step":{"type":"function_call","id":"call_lookup","name":"lookup"}}

data: {"event_type":"step.delta","index":0,"delta":{"type":"arguments_delta","arguments":"{\"q\":"}}

data: {"event_type":"step.delta","index":0,"delta":{"type":"arguments_delta","arguments":"\"x\"}"}}

data: {"event_type":"step.stop","index":0}

data: {"event_type":"interaction.completed","interaction":{"id":"interaction_1","status":"requires_action"}}

`
	stream := decodeGeminiStreamForProviderRequest(t, provider.Request{ExchangeID: "ex", Canonical: request, ToolNames: names, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)}, raw)
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "resp"})), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	call, ok := response.Items()[0].ToolCall()
	if !ok {
		t.Fatal("missing call")
	}
	object, ok := call.Input().Object()
	if !ok || object.String() != `{"q":"x"}` {
		t.Fatalf("arguments = %s/%t", object.String(), ok)
	}
}

func TestInteractionsStreamCapturesOnlyPersistenceEligibleInteractionHandles(t *testing.T) {
	raw := `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"interaction.completed","interaction":{"id":"interaction_1","status":"completed"}}

`
	for _, tc := range []struct {
		name       string
		store      canonical.Specified[bool]
		wantHandle bool
	}{
		{name: "omitted store", store: canonical.Unspecified[bool](), wantHandle: true},
		{name: "store true", store: canonical.Specify(true), wantHandle: true},
		{name: "store false", store: canonical.Specify(false)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := canonical.NewCanonicalRequest(canonical.RequestParams{
				Model: canonical.Specify("gemini-model"), Store: tc.store,
				Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
			})
			stream := decodeGeminiStreamForRequest(t, request, raw)
			bound := canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "resp_gemini", TargetID: "gemini-target", TargetVersion: 4})
			closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(bound), canonical.EnvResponse)
			if err != nil {
				t.Fatal(err)
			}
			response, err := closed.ProjectResponse()
			if err != nil {
				t.Fatal(err)
			}
			got := response.Response().Interactions
			if (got != nil) != tc.wantHandle {
				t.Fatalf("Interactions = %#v, want present=%t", got, tc.wantHandle)
			}
			if got != nil && (got.ProviderInteractionID != "interaction_1" || got.TargetID != "gemini-target" || got.TargetVersion != 4) {
				t.Fatalf("bound continuation = %#v", got)
			}
		})
	}
}

func TestInteractionsStreamRejectsInvalidStepOrdering(t *testing.T) {
	created := `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}\n\n`
	for name, suffix := range map[string]string{
		"delta before start":   `data: {"event_type":"step.delta","index":0,"delta":{"type":"text","text":"late"}}\n\n`,
		"non-contiguous start": `data: {"event_type":"step.start","index":1,"step":{"type":"model_output"}}\n\n`,
		"negative index":       `data: {"event_type":"step.start","index":-1,"step":{"type":"model_output"}}\n\n`,
		"duplicate start":      `data: {"event_type":"step.start","index":0,"step":{"type":"model_output"}}\n\ndata: {"event_type":"step.start","index":0,"step":{"type":"model_output"}}\n\n`,
	} {
		t.Run(name, func(t *testing.T) {
			stream := decodeGeminiStream(t, created+suffix)
			for {
				_, err := stream.Next(context.Background())
				if err != nil {
					var backend canonical.BackendError
					if !errors.As(err, &backend) {
						t.Fatalf("error = %T %v, want backend failure", err, err)
					}
					break
				}
			}
		})
	}
}

func TestInteractionsStreamRejectsInvalidLifecycleIdentityAndStatus(t *testing.T) {
	tests := map[string]string{
		"created status":   `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"completed"}}\n\n`,
		"completed ID":     `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}\n\ndata: {"event_type":"interaction.completed","interaction":{"id":"interaction_other","status":"completed"}}\n\n`,
		"completed status": `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}\n\ndata: {"event_type":"interaction.completed","interaction":{"id":"interaction_1","status":"in_progress"}}\n\n`,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			stream := decodeGeminiStream(t, raw)
			for {
				_, err := stream.Next(context.Background())
				if err == nil {
					continue
				}
				var backend canonical.BackendError
				if !errors.As(err, &backend) {
					t.Fatalf("error = %T %v, want backend lifecycle failure", err, err)
				}
				break
			}
		})
	}
}

func TestInteractionsStreamRejectsResidualKnownUnsupportedSemantics(t *testing.T) {
	tests := map[string]string{
		"function result step": `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}\n\ndata: {"event_type":"step.start","index":0,"step":{"type":"function_result"}}\n\n`,
		"image delta":          `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}\n\ndata: {"event_type":"step.start","index":0,"step":{"type":"model_output"}}\n\ndata: {"event_type":"step.delta","index":0,"delta":{"type":"image"}}\n\n`,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			stream := decodeGeminiStream(t, raw)
			for {
				_, err := stream.Next(context.Background())
				if err != nil {
					var notImplemented canonical.Error
					if !errors.As(err, &notImplemented) || notImplemented.Code != canonical.ErrorCodeNotImplemented {
						t.Fatalf("error = %T %v, want NOT_IMPLEMENTED", err, err)
					}
					break
				}
			}
		})
	}
}

func TestInteractionsStreamRejectsUnknownStepTypeBecausePublishedUnionIsClosed(t *testing.T) {
	stream := decodeGeminiStream(t, `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"step.start","index":0,"step":{"type":"future_step"}}

`)
	for {
		_, err := stream.Next(context.Background())
		if err == nil {
			continue
		}
		var backend canonical.BackendError
		if !errors.As(err, &backend) || !strings.Contains(backend.Message, "unknown step") {
			t.Fatalf("error = %T %v, want backend unknown-step failure", err, err)
		}
		return
	}
}

func TestInteractionsStreamValidatesStatusUpdatesAndAdmitsPendingAction(t *testing.T) {
	t.Run("in progress", func(t *testing.T) {
		stream := decodeGeminiStream(t, `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"interaction.status_update","interaction_id":"interaction_1","status":"in_progress"}

data: {"event_type":"interaction.completed","interaction":{"id":"interaction_1","status":"completed"}}

`)
		closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "resp_status"})), canonical.EnvResponse)
		if err != nil {
			t.Fatal(err)
		}
		response, err := closed.ProjectResponse()
		if err != nil {
			t.Fatal(err)
		}
		if response.Completion().Class() != canonical.CompletionCompleted {
			t.Fatalf("completion = %#v", response.Completion())
		}
	})

	for name, raw := range map[string]string{
		"wrong interaction": `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"interaction.status_update","interaction_id":"interaction_other","status":"in_progress"}

`,
		"terminal status before completion": `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"interaction.status_update","interaction_id":"interaction_1","status":"completed"}

`,
	} {
		t.Run(name, func(t *testing.T) {
			stream := decodeGeminiStream(t, raw)
			for {
				_, err := stream.Next(context.Background())
				if err == nil {
					continue
				}
				if name == "wrong interaction" || name == "terminal status before completion" {
					var backend canonical.BackendError
					if !errors.As(err, &backend) || !strings.Contains(backend.Message, "status update") {
						t.Fatalf("error = %T %v, want backend status-update failure", err, err)
					}
				}
				return
			}
		})
	}
}

func TestInteractionsStreamCompletesRequiresActionOnlyAfterFunctionCall(t *testing.T) {
	key := canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "lookup")
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gemini-model"),
		Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, canonicaltest.MustFunctionTool(key, "", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool]()))},
	})
	names, _, err := provider.BuildAttemptToolNames(request)
	if err != nil {
		t.Fatal(err)
	}
	stream := decodeGeminiStreamForProviderRequest(t, provider.Request{ExchangeID: "gemini-exchange", Canonical: request, ToolNames: names, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)}, `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"step.start","index":0,"step":{"type":"function_call","id":"call_lookup","name":"lookup","arguments":{"q":"x"}}}

data: {"event_type":"step.stop","index":0}

data: {"event_type":"interaction.completed","interaction":{"id":"interaction_1","status":"requires_action"}}

`)
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "resp_requires_action"})), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	if response.Completion().Class() != canonical.CompletionCompleted || response.Completion().Reason() != "requires_action" {
		t.Fatalf("completion = %#v", response.Completion())
	}
	items := response.Items()
	if len(items) != 1 {
		t.Fatalf("items = %#v", items)
	}
	call, ok := items[0].ToolCall()
	if !ok || call.CallID().String() != "call_lookup" || call.Tool() != key {
		t.Fatalf("call = %#v", items[0])
	}

	stream = decodeGeminiStream(t, `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"interaction.completed","interaction":{"id":"interaction_1","status":"requires_action"}}

`)
	for {
		_, err := stream.Next(context.Background())
		if err == nil {
			continue
		}
		var backend canonical.BackendError
		if !errors.As(err, &backend) || !strings.Contains(backend.Message, "without a function call") {
			t.Fatalf("error = %T %v, want backend contradiction", err, err)
		}
		return
	}
}

func TestInteractionsStreamProjectsGoogleSearchLifecycleAndURLCitations(t *testing.T) {
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gemini-model"),
		Items: []canonical.CanonicalItem{
			canonicaltest.ToolDeclarations(t, canonical.NewWebSearchDeclaration()),
			canonicaltest.Message(t, canonical.MessageRoleUser, "find the source"),
		},
	})
	stream := decodeGeminiStreamForRequest(t, request, `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"step.start","index":0,"step":{"type":"google_search_call","id":"search_1","search_type":"web_search"}}

data: {"event_type":"step.delta","index":0,"delta":{"type":"google_search_call","arguments":{"queries":["swobu","swobu"]},"signature":"opaque-search-signature"}}

data: {"event_type":"step.stop","index":0}

data: {"event_type":"step.start","index":1,"step":{"type":"google_search_result","call_id":"search_1"}}

data: {"event_type":"step.delta","index":1,"delta":{"type":"google_search_result","result":[{"search_suggestions":"synthetic-redacted-live-suggestion"}],"is_error":false,"signature":"opaque-result-signature"}}

data: {"event_type":"step.stop","index":1}

data: {"event_type":"step.start","index":2,"step":{"type":"model_output"}}

data: {"event_type":"step.delta","index":2,"delta":{"type":"text","text":"A£B"}}

data: {"event_type":"step.delta","index":2,"delta":{"type":"text_annotation_delta","annotations":[{"type":"url_citation","url":"https://example.test/source","title":"Example","start_index":1,"end_index":3,"snippet":"£"}]}}

data: {"event_type":"step.stop","index":2}

data: {"event_type":"interaction.completed","interaction":{"id":"interaction_1","status":"completed"}}

`)
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "resp_search"})), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	items := response.Items()
	if len(items) != 3 {
		t.Fatalf("items = %#v, want search call, result, and message", items)
	}
	call, ok := items[0].ToolCall()
	if !ok || call.CallID().String() != "search_1" || call.Tool() != canonical.WebSearchToolKey() {
		t.Fatalf("search call = %#v", items[0])
	}
	search, ok := call.Input().WebSearch()
	if !ok || search.Action != canonical.WebSearchActionSearch || strings.Join(search.Queries, ",") != "swobu,swobu" {
		t.Fatalf("search input = %#v", call.Input())
	}
	result, ok := items[1].ToolResult()
	if !ok || result.CallID() != call.CallID() {
		t.Fatalf("search result = %#v", items[1])
	}
	searchResult, ok := result.WebSearch()
	if !ok || len(searchResult.Sources()) != 0 {
		t.Fatalf("canonical search result = %#v", result)
	}
	message, ok := items[2].Message()
	if !ok || len(message.Content()) != 1 {
		t.Fatalf("message = %#v", items[2])
	}
	text, _ := message.Content()[0].Text()
	citations := message.Content()[0].Citations()
	if text.Text() != "A£B" || len(citations) != 1 {
		t.Fatalf("text/citations = %q/%#v", text.Text(), citations)
	}
	start, hasStart := citations[0].Start.Get()
	end, hasEnd := citations[0].End.Get()
	if !hasStart || !hasEnd || start != 1 || end != 3 {
		t.Fatalf("citation UTF-8 offsets = %d/%d present=%t/%t", start, end, hasStart, hasEnd)
	}
	if excerpt, ok := citations[0].Excerpt.Get(); !ok || excerpt != "£" {
		t.Fatalf("citation excerpt = %q/%t", excerpt, ok)
	}
	stateless := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("gemini-model"), Store: canonical.Specify(false), Items: append([]canonical.CanonicalItem{canonicaltest.ToolDeclarations(t, canonical.NewWebSearchDeclaration())}, items...)})
	document, _, err := (codec{}).Encode(provider.Request{Canonical: stateless, Delivery: delivery.StreamingDelivery(delivery.FramingSSE)})
	if err != nil {
		t.Fatal(err)
	}
	wire := string(document.RawBytes())
	for _, want := range []string{"opaque-search-signature", "opaque-result-signature", `"type":"google_search_call"`, `"type":"google_search_result"`} {
		if !strings.Contains(wire, want) {
			t.Fatalf("stateless Search replay missing %q: %s", want, wire)
		}
	}
}

func TestInteractionsStreamCapturesSignatureOnlyThoughtWithoutFabricatedSummary(t *testing.T) {
	stream := decodeGeminiStream(t, `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"step.start","index":0,"step":{"type":"thought"}}

data: {"event_type":"step.delta","index":0,"delta":{"type":"thought_signature","signature":"thought-secret"}}

data: {"event_type":"step.stop","index":0}

data: {"event_type":"interaction.completed","interaction":{"id":"interaction_1","status":"completed"}}

`)
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "resp_thought"})), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	items := response.Items()
	if len(items) != 1 {
		t.Fatalf("items = %#v, want one opaque reasoning occurrence", items)
	}
	reasoning, ok := items[0].Reasoning()
	if !ok || len(reasoning.Parts()) != 0 {
		t.Fatalf("reasoning = %#v, want signature-only occurrence", items[0])
	}
	raw, ok := reasoning.Opaque().Interactions()
	if !ok || !jsonEqual(raw, []byte(`{"type":"thought","signature":"thought-secret"}`)) {
		t.Fatalf("opaque replay = %s/%t", raw, ok)
	}
	var step map[string]json.RawMessage
	if err := json.Unmarshal(raw, &step); err != nil {
		t.Fatal(err)
	}
	if _, fabricated := step["summary"]; fabricated {
		t.Fatalf("signature-only capture fabricated summary: %s", raw)
	}
}

func TestInteractionsStreamRejectsUnparsedRichGoogleSearchResultInsteadOfEmptySuccess(t *testing.T) {
	stream := decodeGeminiStream(t, `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"step.start","index":0,"step":{"type":"google_search_call","id":"search_1","search_type":"web_search"}}

data: {"event_type":"step.delta","index":0,"delta":{"type":"google_search_call","arguments":{"queries":["swobu"]},"signature":"call-signature"}}

data: {"event_type":"step.stop","index":0}

data: {"event_type":"step.start","index":1,"step":{"type":"google_search_result","call_id":"search_1"}}

data: {"event_type":"step.delta","index":1,"delta":{"type":"google_search_result","result":{"sources":[{"url":"https://example.test/source","title":"Example"}]},"is_error":false,"signature":"result-signature"}}

data: {"event_type":"step.stop","index":1}

`)
	for {
		_, err := stream.Next(context.Background())
		if err == nil {
			continue
		}
		var backend canonical.BackendError
		if !errors.As(err, &backend) || !strings.Contains(backend.Message, "result shape") {
			t.Fatalf("error = %T %v, want explicit backend result-shape failure", err, err)
		}
		return
	}
}

func TestInteractionsStreamAcceptsOnlyCapturedCurrentWireSearchResultRows(t *testing.T) {
	for name, result := range map[string]string{
		"multiple suggestion rows": `[{"search_suggestions":"first"},{"search_suggestions":"second"}]`,
		"empty rows":               `[]`,
		"unknown row field":        `[{"future_field":"value"}]`,
		"mixed known unknown":      `[{"search_suggestions":"value","future_field":true}]`,
		"non string suggestion":    `[{"search_suggestions":["value"]}]`,
	} {
		t.Run(name, func(t *testing.T) {
			stream := decodeGeminiStream(t, `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"step.start","index":0,"step":{"type":"google_search_call","id":"search_1","search_type":"web_search"}}

data: {"event_type":"step.delta","index":0,"delta":{"type":"google_search_call","arguments":{"queries":["q"]},"signature":"call-signature"}}

data: {"event_type":"step.stop","index":0}

data: {"event_type":"step.start","index":1,"step":{"type":"google_search_result","call_id":"search_1"}}

data: {"event_type":"step.delta","index":1,"delta":{"type":"google_search_result","result":`+result+`,"is_error":false,"signature":"result-signature"}}

data: {"event_type":"step.stop","index":1}

data: {"event_type":"interaction.completed","interaction":{"id":"interaction_1","status":"completed"}}

`)
			_, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "resp_shape"})), canonical.EnvResponse)
			if name == "multiple suggestion rows" {
				if err != nil {
					t.Fatalf("captured current-wire row shape rejected: %v", err)
				}
				return
			}
			var backend canonical.BackendError
			if !errors.As(err, &backend) || !strings.Contains(backend.Message, "result shape") {
				t.Fatalf("error = %T %v, want fail-closed result-shape error", err, err)
			}
		})
	}
}

func TestInteractionsStreamRejectsMalformedOrUnsupportedGoogleSearchFrames(t *testing.T) {
	for name, raw := range map[string]string{
		"missing call id": `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"step.start","index":0,"step":{"type":"google_search_call","arguments":{"queries":["q"]}}}

`,
		"image search": `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"step.start","index":0,"step":{"type":"google_search_call","id":"search_1","search_type":"image_search","arguments":{"queries":["q"]}}}

`,
		"result without call": `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"step.start","index":0,"step":{"type":"google_search_result","call_id":"search_1","result":[]}}

`,
		"call without signature": `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"step.start","index":0,"step":{"type":"google_search_call","id":"search_1","search_type":"web_search"}}

data: {"event_type":"step.delta","index":0,"delta":{"type":"google_search_call","arguments":{"queries":["q"]}}}

data: {"event_type":"step.stop","index":0}

`,
		"call signature contradiction": `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"step.start","index":0,"step":{"type":"google_search_call","id":"search_1","search_type":"web_search","signature":"start-signature"}}

data: {"event_type":"step.delta","index":0,"delta":{"type":"google_search_call","arguments":{"queries":["q"]},"signature":"different-signature"}}

`,
		"result without signature": `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"step.start","index":0,"step":{"type":"google_search_call","id":"search_1","search_type":"web_search","signature":"call-signature"}}

data: {"event_type":"step.delta","index":0,"delta":{"type":"google_search_call","arguments":{"queries":["q"]},"signature":"call-signature"}}

data: {"event_type":"step.stop","index":0}

data: {"event_type":"step.start","index":1,"step":{"type":"google_search_result","call_id":"search_1"}}

data: {"event_type":"step.delta","index":1,"delta":{"type":"google_search_result","result":[{"search_suggestions":"suggestion"}],"is_error":false}}

data: {"event_type":"step.stop","index":1}

`,
		"result signature contradiction": `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"step.start","index":0,"step":{"type":"google_search_call","id":"search_1","search_type":"web_search","signature":"call-signature"}}

data: {"event_type":"step.delta","index":0,"delta":{"type":"google_search_call","arguments":{"queries":["q"]},"signature":"call-signature"}}

data: {"event_type":"step.stop","index":0}

data: {"event_type":"step.start","index":1,"step":{"type":"google_search_result","call_id":"search_1","signature":"start-signature"}}

data: {"event_type":"step.delta","index":1,"delta":{"type":"google_search_result","result":[{"search_suggestions":"suggestion"}],"is_error":false,"signature":"different-signature"}}

`,
		"offset inside rune": `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"step.start","index":0,"step":{"type":"model_output"}}

data: {"event_type":"step.delta","index":0,"delta":{"type":"text","text":"A£B"}}

data: {"event_type":"step.delta","index":0,"delta":{"type":"text_annotation_delta","annotation":{"type":"url_citation","url":"https://example.test/source","start_index":2,"end_index":3}}}

`,
		"provider MCP lifecycle": `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"step.start","index":0,"step":{"type":"mcp_server_tool_call","id":"mcp_1","name":"read","arguments":{},"server_name":"docs"}}

`,
	} {
		t.Run(name, func(t *testing.T) {
			stream := decodeGeminiStream(t, raw)
			for {
				_, err := stream.Next(context.Background())
				if err == nil {
					continue
				}
				if name == "image search" || name == "provider MCP lifecycle" {
					var notImplemented canonical.Error
					if !errors.As(err, &notImplemented) || notImplemented.Code != canonical.ErrorCodeNotImplemented {
						t.Fatalf("error = %T %v, want NOT_IMPLEMENTED", err, err)
					}
				} else {
					var backend canonical.BackendError
					if !errors.As(err, &backend) {
						t.Fatalf("error = %T %v, want backend failure", err, err)
					}
				}
				return
			}
		})
	}
}

func TestInteractionsStreamRejectsUnknownTextDeltaInsteadOfOmittingWholeItem(t *testing.T) {
	stream := decodeGeminiStream(t, `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"step.start","index":0,"step":{"type":"model_output"}}

data: {"event_type":"step.delta","index":0,"delta":{"type":"future_delta"}}

`)
	for {
		_, err := stream.Next(context.Background())
		if err == nil {
			continue
		}
		var backend canonical.BackendError
		if !errors.As(err, &backend) || !strings.Contains(backend.Message, "unknown delta") {
			t.Fatalf("error = %T %v, want backend unknown-delta failure", err, err)
		}
		break
	}
	if changes := stream.Changes(); len(changes) != 0 {
		t.Fatalf("changes = %#v, must not claim whole-item omission", changes)
	}
}

func TestInteractionsStreamMapsIncompleteAndBackendErrorTerminals(t *testing.T) {
	t.Run("incomplete", func(t *testing.T) {
		stream := decodeGeminiStream(t, `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"interaction.completed","interaction":{"id":"interaction_1","status":"incomplete","usage":{"total_input_tokens":1,"total_output_tokens":0}}}

`)
		closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "resp_incomplete"})), canonical.EnvResponse)
		if err != nil {
			t.Fatal(err)
		}
		response, err := closed.ProjectResponse()
		if err != nil {
			t.Fatal(err)
		}
		if response.Completion().Class() != canonical.CompletionIncomplete {
			t.Fatalf("completion = %#v", response.Completion())
		}
	})

	t.Run("error before creation", func(t *testing.T) {
		stream := decodeGeminiStream(t, `data: {"event_type":"error","error":{"code":"permission_denied","message":"denied"}}

`)
		_, err := stream.Next(context.Background())
		var backend canonical.BackendError
		if !errors.As(err, &backend) || backend.Message != "denied" {
			t.Fatalf("error = %T %v, want backend error before envelope", err, err)
		}
	})

	t.Run("error after creation", func(t *testing.T) {
		stream := decodeGeminiStream(t, `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

data: {"event_type":"error","error":{"code":"permission_denied","message":"denied"}}

`)
		bound := canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "resp_error"})
		closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(bound), canonical.EnvResponse)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := closed.ProjectResponse(); err == nil || !strings.Contains(err.Error(), "permission_denied") {
			t.Fatalf("ProjectResponse error = %v, want terminal provider error", err)
		}
	})
}

func TestInteractionsStreamReportsUnexpectedEOFAsTerminalError(t *testing.T) {
	stream := decodeGeminiStream(t, `data: {"event_type":"interaction.created","interaction":{"id":"interaction_1","model":"gemini-model","status":"in_progress"}}

`)
	bound := canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "resp_eof"})
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(bound), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := closed.ProjectResponse(); err == nil || !strings.Contains(err.Error(), "stream_unexpected_eof") {
		t.Fatalf("ProjectResponse error = %v, want terminal unexpected-eof error", err)
	}
}

func decodeGeminiStream(t *testing.T, raw string) *interactionsStream {
	t.Helper()
	return decodeGeminiStreamForRequest(t, canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("gemini-model"), Items: []canonical.CanonicalItem{canonicaltest.Message(t, canonical.MessageRoleUser, "hello")},
	}), raw)
}

func decodeGeminiStreamForRequest(t *testing.T, request canonical.CanonicalRequest, raw string) *interactionsStream {
	t.Helper()
	return decodeGeminiStreamForProviderRequest(t, provider.Request{
		ExchangeID: "gemini-exchange",
		Canonical:  request,
		Delivery:   delivery.StreamingDelivery(delivery.FramingSSE),
	}, raw)
}

func decodeGeminiStreamForProviderRequest(t *testing.T, request provider.Request, raw string) *interactionsStream {
	t.Helper()
	raw = strings.ReplaceAll(raw, `\n`, "\n")
	decoded, err := (codec{}).Decode(context.Background(), request, provider.StreamIngress{Stream: carrier.ByteStream{
		MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw)),
	}})
	if err != nil {
		t.Fatal(err)
	}
	stream, ok := decoded.Stream.(*interactionsStream)
	if !ok {
		t.Fatalf("decoded stream = %T, want *interactionsStream", decoded.Stream)
	}
	return stream
}

func assertUsage(t *testing.T, usage canonical.TokenUsage, input, output, reasoning, cached int) {
	t.Helper()
	if got, ok := usage.InputTokens(); !ok || got != input {
		t.Fatalf("input usage = %d/%t, want %d", got, ok, input)
	}
	if got, ok := usage.OutputTokens(); !ok || got != output {
		t.Fatalf("output usage = %d/%t, want %d", got, ok, output)
	}
	if got, ok := usage.ReasoningTokens(); !ok || got != reasoning {
		t.Fatalf("reasoning usage = %d/%t, want %d", got, ok, reasoning)
	}
	if got, ok := usage.CacheReadTokens(); !ok || got != cached {
		t.Fatalf("cached usage = %d/%t, want %d", got, ok, cached)
	}
}
