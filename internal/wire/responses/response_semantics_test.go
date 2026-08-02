package responses

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestResponsesDocumentAllUnknownMessagePartsRejectEmptyResidual(t *testing.T) {
	raw := []json.RawMessage{json.RawMessage(`{"type":"message","role":"assistant","content":[{"type":"future_content"}]}`)}
	if _, err := decodeCompletedResponsesItemSet(context.Background(), canonical.CanonicalRequest{}, nil, raw, "", "", nil); err == nil {
		t.Fatal("all-erased message output was reported as successful")
	}
}

func TestResponsesDocumentRejectsMissingOutputItemType(t *testing.T) {
	raw := []byte(`{
		"id":"resp_1",
		"model":"m",
		"status":"completed",
		"output":[
			{"id":"broken","content":[{"type":"output_text","text":"hidden"}]},
			{"type":"message","status":"completed","content":[{"type":"output_text","text":"visible"}]}
		]
	}`)
	if _, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, nil, raw, "ex", nil, true); err == nil {
		t.Fatal("missing output item type was erased as an additive variant")
	}
}

func TestResponsesDocumentRejectsActiveOutputItemStatus(t *testing.T) {
	for _, status := range []string{"in_progress", "banana"} {
		t.Run(status, func(t *testing.T) {
			raw := []byte(`{
				"id":"resp_1",
				"model":"m",
				"status":"completed",
				"output":[
					{"type":"message","status":"` + status + `","content":[{"type":"output_text","text":"partial"}]}
				]
			}`)
			if _, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, nil, raw, "ex", nil, true); err == nil {
				t.Fatal("invalid output item status was accepted in a terminal response")
			}
		})
	}
}

func TestResponsesProviderOutputRejectsMissingNestedDiscriminators(t *testing.T) {
	tests := []struct {
		name string
		item string
	}{
		{
			name: "message content",
			item: `{"type":"message","status":"completed","content":[{"text":"hidden"},{"type":"output_text","text":"visible"}]}`,
		},
		{
			name: "reasoning summary",
			item: `{"type":"reasoning","status":"completed","summary":[{"text":"hidden"},{"type":"summary_text","text":"visible"}]}`,
		},
		{
			name: "reasoning trace",
			item: `{"type":"reasoning","status":"completed","content":[{"text":"hidden"},{"type":"reasoning_text","text":"visible"}]}`,
		},
		{
			name: "annotation",
			item: `{"type":"message","status":"completed","content":[{"type":"output_text","text":"visible","annotations":[{"url":"https://hidden.test"},{"type":"url_citation","url":"https://visible.test","start_index":0,"end_index":0}]}]}`,
		},
		{
			name: "web search source",
			item: `{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","queries":["q"],"sources":[{"url":"https://hidden.test"},{"type":"url","url":"https://visible.test"}]}}`,
		},
		{
			name: "discovery tool declaration",
			item: `{"type":"tool_search_output","call_id":"search_1","status":"completed","execution":"server","tools":[{"name":"hidden","parameters":{}},{"type":"function","name":"visible","parameters":{}}]}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := []json.RawMessage{json.RawMessage(test.item)}
			if _, err := decodeCompletedResponsesItemSet(context.Background(), canonical.CanonicalRequest{}, nil, raw, "", "ex", nil); err == nil {
				t.Fatal("missing provider-output child type was erased as an additive variant")
			}
		})
	}
}

func TestResponsesProviderDiscoveryResultRejectsAllErasedDeclarations(t *testing.T) {
	request := responsesDiscoveryRequest(t)
	raw := []json.RawMessage{
		json.RawMessage(`{"type":"tool_search_call","call_id":"search_1","status":"completed","execution":"server","arguments":{}}`),
		json.RawMessage(`{"type":"tool_search_output","call_id":"search_1","status":"completed","execution":"server","tools":[{"type":"future_tool"}]}`),
	}
	if _, err := decodeCompletedResponsesItemSet(context.Background(), request, nil, raw, "", "ex", nil); err == nil {
		t.Fatal("all-erased provider discovery result fabricated an empty result")
	}
}

func TestResponsesCompletedResponseRejectsUnsuccessfulEffectItems(t *testing.T) {
	tests := []struct {
		name    string
		request canonical.CanonicalRequest
		item    string
	}{
		{
			name:    "incomplete function call",
			request: responsesFunctionRequest(t),
			item:    `{"type":"function_call","call_id":"call_1","name":"lookup","status":"incomplete","arguments":"{}"}`,
		},
		{
			name:    "incomplete discovery call",
			request: responsesDiscoveryRequest(t),
			item:    `{"type":"tool_search_call","call_id":"search_1","execution":"server","status":"incomplete","arguments":{}}`,
		},
		{
			name:    "incomplete discovery result",
			request: responsesDiscoveryRequest(t),
			item:    `{"type":"tool_search_output","call_id":"search_1","execution":"server","status":"incomplete","tools":[]}`,
		},
		{
			name: "failed message",
			item: `{"type":"message","status":"failed","content":[{"type":"output_text","text":"not successful"}]}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := []byte(`{"id":"resp_1","model":"m","status":"completed","output":[` + test.item + `]}`)
			if _, err := decodeResponseBuffered(context.Background(), test.request, testAttemptToolNames(test.request), raw, "ex", nil, true); err == nil {
				t.Fatal("unsuccessful item became successful canonical output")
			}
		})
	}
}

func TestResponsesIncompleteResponsePreservesPermittedPartialMessage(t *testing.T) {
	raw := []byte(`{
		"id":"resp_1",
		"model":"m",
		"status":"incomplete",
		"output":[
			{"type":"message","status":"incomplete","content":[{"type":"output_text","text":"partial"}]}
		]
	}`)
	stream, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, nil, raw, "ex", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	var completed int
	var finish string
	for {
		event, nextErr := stream.Next(context.Background())
		if nextErr == io.EOF {
			break
		}
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		switch event.Kind {
		case canonical.EventItemCompleted:
			completed++
		case canonical.EventFinish:
			finish = event.Payload.(canonical.FinishPayload).Completion.Reason()
		}
	}
	if completed != 1 || finish != "incomplete" {
		t.Fatalf("partial response completed=%d finish=%q, want one item with incomplete finish", completed, finish)
	}
}

func TestResponsesFailedWebSearchProjectsTypedFailure(t *testing.T) {
	raw := []byte(`{
		"id":"resp_1",
		"model":"m",
		"status":"completed",
		"output":[{
			"type":"web_search_call",
			"id":"ws_1",
			"status":"failed",
			"action":{"type":"search","queries":["q"],"sources":[]}
		}]
	}`)
	reader, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, nil, raw, "ex", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := canonical.ReadClosedEnvelope(
		context.Background(),
		canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}),
		canonical.EnvResponse,
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	items := response.Items()
	if len(items) != 2 || items[0].Kind() != canonical.ItemKindToolCall {
		t.Fatalf("failed lifecycle items = %#v", items)
	}
	result, ok := items[1].ToolResult()
	if !ok {
		t.Fatalf("failed lifecycle result = %#v", items[1])
	}
	search, ok := result.WebSearch()
	failure, failed := search.Failure()
	if !ok || !failed || failure != "provider reported failed web search" {
		t.Fatalf("typed failure = (%q,%t,%t)", failure, failed, ok)
	}
}

func TestResponsesFailedResponseCannotCarrySuccessfulItems(t *testing.T) {
	raw := []byte(`{
		"id":"resp_1",
		"model":"m",
		"status":"failed",
		"output":[
			{"type":"message","status":"completed","content":[{"type":"output_text","text":"not successful"}]}
		]
	}`)
	if _, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, nil, raw, "ex", nil, true); err == nil {
		t.Fatal("failed response carried a successful canonical message")
	}
}

func TestResponsesProviderDiscoveryValidationErrorsRemainBackendOrigin(t *testing.T) {
	tests := []struct {
		name string
		tool string
	}{
		{name: "function missing name", tool: `{"type":"function","parameters":{}}`},
		{name: "custom missing name", tool: `{"type":"custom","format":{"type":"text"}}`},
		{name: "namespace missing name", tool: `{"type":"namespace","tools":[{"type":"function","name":"child","parameters":{}}]}`},
		{name: "namespace invalid child", tool: `{"type":"namespace","name":"n","tools":[{"type":"function","parameters":{}}]}`},
		{name: "tool search invalid execution", tool: `{"type":"tool_search","execution":"banana","description":"find","parameters":{}}`},
		{name: "web search malformed domain", tool: `{"type":"web_search","filters":{"allowed_domains":[""]}}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := []json.RawMessage{json.RawMessage(`{
				"type":"tool_search_output",
				"call_id":"search_1",
				"status":"completed",
				"execution":"server",
				"tools":[` + test.tool + `]
			}`)}
			_, err := decodeCompletedResponsesItemSet(context.Background(), responsesDiscoveryRequest(t), nil, raw, "", "ex", nil)
			if err == nil {
				t.Fatal("malformed provider declaration was accepted")
			}
			var backendErr canonical.BackendError
			if !errors.As(err, &backendErr) {
				t.Fatalf("provider declaration error = %v, want backend origin", err)
			}
		})
	}
}

func TestResponsesDiscoveryResultUsesProviderItemCompatibilityFeature(t *testing.T) {
	raw := []json.RawMessage{json.RawMessage(`{
		"type":"tool_search_output",
		"call_id":"search_1",
		"execution":"server",
		"tools":[
			{"type":"future_tool","name":"ignored"},
			{"type":"function","name":"kept","parameters":{"type":"object"}}
		]
	}`)}
	changeLog := &recordingChanges{}
	items, err := decodeCompletedResponsesItemSet(context.Background(), canonical.CanonicalRequest{}, nil, raw, "", "ex", changeLog)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Kind() != canonical.ItemKindToolDiscoveryResult {
		t.Fatalf("canonical items = %#v, want one discovery result", items)
	}
	changes := *changeLog
	if len(changes) != 1 {
		t.Fatalf("compatibility changes = %#v, want provider discovery-result evidence", changes)
	}
	item, occurrenceOK := changes[0].Occurrence.ToolIndex()
	if changes[0].Capability != canonical.ResponseItemsKind ||
		changes[0].Kind != compat.Omission ||
		!occurrenceOK || item != 0 {
		t.Fatalf("compatibility changes = %#v, want provider discovery-result evidence", changes)
	}
}

func TestResponsesDiscoveryWebSearchFieldsUseProviderItemCompatibilityFeature(t *testing.T) {
	raw := []json.RawMessage{json.RawMessage(`{
		"type":"tool_search_output",
		"call_id":"search_1",
		"execution":"server",
		"tools":[
			{"type":"web_search","search_context_size":"high"}
		]
	}`)}
	changeLog := &recordingChanges{}
	items, err := decodeCompletedResponsesItemSet(context.Background(), canonical.CanonicalRequest{}, nil, raw, "", "ex", changeLog)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Kind() != canonical.ItemKindToolDiscoveryResult {
		t.Fatalf("canonical items = %#v, want one discovery result", items)
	}
	changes := *changeLog
	if len(changes) != 1 {
		t.Fatalf("compatibility changes = %#v, want provider web-search declaration evidence", changes)
	}
	item, occurrenceOK := changes[0].Occurrence.ToolIndex()
	if changes[0].Capability != canonical.ResponseItemsKind ||
		changes[0].Kind != compat.Approximation ||
		!occurrenceOK || item != 0 {
		t.Fatalf("compatibility changes = %#v, want provider web-search declaration evidence", changes)
	}
}

func TestResponsesDiscoveryClassifiesFutureWebSearchEnumsLocally(t *testing.T) {
	tests := []struct {
		name        string
		tool        string
		wantError   bool
		wantOutcome compat.Kind
	}{
		{name: "content type", tool: `{"type":"web_search","search_content_types":["video"]}`, wantError: true, wantOutcome: compat.Omission},
		{name: "location type", tool: `{"type":"web_search","user_location":{"type":"future_location"}}`, wantError: true, wantOutcome: compat.Omission},
		{name: "context size", tool: `{"type":"web_search","search_context_size":"ultra"}`, wantOutcome: compat.Approximation},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := []json.RawMessage{json.RawMessage(`{
				"type":"tool_search_output",
				"call_id":"search_1",
				"execution":"server",
				"tools":[` + test.tool + `]
			}`)}
			changeLog := &recordingChanges{}
			_, err := decodeCompletedResponsesItemSet(context.Background(), canonical.CanonicalRequest{}, nil, raw, "", "ex", changeLog)
			var backendErr canonical.BackendError
			if test.wantError && !errors.As(err, &backendErr) {
				t.Fatalf("error = %T %v, want backend error", err, err)
			}
			if !test.wantError && err != nil {
				t.Fatal(err)
			}
			if len(*changeLog) != 1 || (*changeLog)[0].Kind != test.wantOutcome {
				t.Fatalf("changes = %#v, want one %v", *changeLog, test.wantOutcome)
			}
		})
	}
}

func TestResponsesDiscoveryDropsUnrepresentedImageSearchOperation(t *testing.T) {
	tests := []struct {
		name string
		tool string
	}{
		{name: "image only", tool: `{"type":"web_search","search_content_types":["image"]}`},
		{name: "mixed text and image", tool: `{"type":"web_search","search_content_types":["text","image"]}`},
		{name: "image output format", tool: `{"type":"web_search","output_format":"image"}`},
		{name: "future image result format", tool: `{"type":"web_search","output_format":"future_image_result"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := []json.RawMessage{json.RawMessage(`{
				"type":"tool_search_output",
				"call_id":"search_1",
				"execution":"server",
				"tools":[` + test.tool + `]
			}`)}
			changeLog := &recordingChanges{}
			_, err := decodeCompletedResponsesItemSet(context.Background(), canonical.CanonicalRequest{}, nil, raw, "", "ex", changeLog)
			var backendErr canonical.BackendError
			if !errors.As(err, &backendErr) {
				t.Fatalf("error = %v, want all-erased discovery backend error", err)
			}
			if len(*changeLog) != 1 || (*changeLog)[0].Kind != compat.Omission {
				t.Fatalf("changes = %#v, want operation Drop", *changeLog)
			}
		})
	}
}

func TestResponsesDocumentPreservesWebSearchCallWhenStatusIsUnknown(t *testing.T) {
	raw := []json.RawMessage{
		json.RawMessage(`{"type":"web_search_call","id":"ws_1","status":"future_status","action":{"type":"search","queries":["q"]}}`),
		json.RawMessage(`{"type":"message","status":"completed","content":[{"type":"output_text","text":"kept"}]}`),
	}
	changeLog := &recordingChanges{}
	items, err := decodeCompletedResponsesItemSet(context.Background(), canonical.CanonicalRequest{}, nil, raw, "", "ex", changeLog)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("canonical items = %#v, want pending web-search call and message", items)
	}
	call, ok := items[0].ToolCall()
	if !ok || call.CallID().String() != "ws_1" {
		t.Fatalf("first item = %#v, want preserved ws_1 call", items[0])
	}
	if _, ok := items[1].Message(); !ok {
		t.Fatalf("second item = %#v, want surviving message", items[1])
	}
	for _, item := range items {
		if _, ok := item.ToolResult(); ok {
			t.Fatalf("unknown lifecycle status manufactured result: %#v", item)
		}
	}
	if len(*changeLog) != 1 {
		t.Fatalf("changes = %#v, want occurrence-local status omission", *changeLog)
	}
	item, occurrenceOK := (*changeLog)[0].Occurrence.ResponseItem()
	if (*changeLog)[0].Kind != compat.Omission || !occurrenceOK || item != 0 {
		t.Fatalf("changes = %#v, want occurrence-local status Drop", *changeLog)
	}
}

func TestResponsesDocumentUnknownWebSearchStatusDefersFailureToSettlement(t *testing.T) {
	raw := []json.RawMessage{
		json.RawMessage(`{"type":"web_search_call","id":"ws_1","status":"future_status","action":{"type":"search","queries":["q"]}}`),
	}
	changeLog := &recordingChanges{}
	items, err := decodeCompletedResponsesItemSet(context.Background(), canonical.CanonicalRequest{}, nil, raw, "", "ex", changeLog)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("canonical items = %#v, want pending web-search call", items)
	}
	if _, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: canonical.NewSwobuResponseID("resp_1")},
		"m",
		items,
		canonical.Completed("completed"),
		canonical.NewUnknownTokenUsage(),
	); err == nil {
		t.Fatal("completed canonical response accepted an unsettled web-search call")
	}
	if len(*changeLog) != 1 {
		t.Fatalf("changes = %#v, want occurrence-local status omission", *changeLog)
	}
	item, occurrenceOK := (*changeLog)[0].Occurrence.ResponseItem()
	if (*changeLog)[0].Kind != compat.Omission || !occurrenceOK || item != 0 {
		t.Fatalf("changes = %#v, want occurrence-local status Drop", *changeLog)
	}
}
