package messages

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestDecodeMessagesWebSearchDropsDynamicCallerPreference(t *testing.T) {
	base := ProviderRequestTool{Type: "web_search_20260209", Name: "web_search"}
	base.AllowedCallers = []string{"code_execution"}
	declaration, err := decodeMessagesWebSearchTool(base)
	if err != nil {
		t.Fatal(err)
	}
	if declaration.Kind() != canonical.ToolKindWebSearch {
		t.Fatal("decoded declaration is not web search")
	}
}

func TestDecodeMessagesWebSearchDropsAllWirePreferences(t *testing.T) {
	raw := []byte(`{"model":"model","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"web_search_20260318","name":"web_search","max_uses":3,"allowed_domains":["example.com"],"blocked_domains":["blocked.example"],"user_location":{"type":"approximate","country":"GB"},"allowed_callers":["code_execution"],"response_inclusion":"full"}]}`)
	decoded, err := (ClientRequestDecoder{}).DecodeClientRequest(carrier.Document{Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	if tools := canonicaltest.Tools(decoded.Request.Request); len(tools) != 1 || tools[0].Kind() != canonical.ToolKindWebSearch {
		t.Fatalf("tools = %#v", tools)
	}
	if len(decoded.Changes) != 0 {
		t.Fatalf("wire-only preferences created semantic changes: %#v", decoded.Changes)
	}
}

func TestEncodeMessagesWebSearchUsesProtocolDefault(t *testing.T) {
	tools, err := encodeMessagesTools([]canonical.ToolDeclaration{canonical.NewWebSearchDeclaration()}, nil, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(tools[0])
	if string(raw) != `{"type":"web_search_20260209","name":"web_search","allowed_callers":["direct"]}` {
		t.Fatalf("tool = %s", raw)
	}
}

func TestDecodeResponseBufferedPreservesWebSearchLifecycle(t *testing.T) {
	raw := []byte(`{"id":"msg_1","model":"m","stop_reason":"end_turn","content":[{"type":"server_tool_use","id":"s","name":"web_search","input":{"query":"x"}},{"type":"web_search_tool_result","tool_use_id":"s","content":[{"type":"web_search_result","url":"https://example.com/x","title":"Example"}]},{"type":"text","text":"answer","citations":[{"type":"web_search_result_location","url":"https://example.com/x","title":"Example"}]}]}`)
	reader, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, nil, raw, "ex", nil)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	items := response.Items()
	if len(items) != 3 || items[0].Kind() != canonical.ItemKindToolCall || items[1].Kind() != canonical.ItemKindToolResult || items[2].Kind() != canonical.ItemKindMessage {
		t.Fatalf("items = %#v", items)
	}
	result, _ := items[1].ToolResult()
	search, ok := result.WebSearch()
	if !ok || len(search.Sources()) != 1 {
		t.Fatalf("search result = %#v, %v", search, ok)
	}
}

func TestDecodeResponseBufferedErasesUnknownWebSearchChildren(t *testing.T) {
	raw := []byte(`{"id":"msg_1","model":"m","stop_reason":"end_turn","content":[
		{"type":"server_tool_use","id":"s","name":"web_search","input":{"query":"x"}},
		{"type":"web_search_tool_result","tool_use_id":"s","content":[
			{"type":"future_result","value":"ignored"},
			{"type":"web_search_result","url":"https://example.com/x","title":"Example"}
		]},
		{"type":"text","text":"answer","citations":[
			{"type":"future_citation","value":"ignored"},
			{"type":"web_search_result_location","url":"https://example.com/x","title":"Example"}
		]}
	]}`)
	var changes []compat.Change
	reader, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, nil, raw, "ex", &changes)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Items()) != 3 {
		t.Fatalf("items = %#v, want call, result, and cited message", response.Items())
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %#v", changes)
	}
	for index, decision := range changes {
		item, ok := decision.Occurrence.ResponseItem()
		if !ok || item != uint32(index+1) || decision.Kind != compat.Omission {
			t.Fatalf("decision = %#v", decision)
		}
	}
}

func TestDecodeResponseBufferedRejectsMissingNestedWebSearchDiscriminators(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "result child",
			raw: `{"id":"msg_1","model":"m","stop_reason":"end_turn","content":[
				{"type":"server_tool_use","id":"s","name":"web_search","input":{"query":"x"}},
				{"type":"web_search_tool_result","tool_use_id":"s","content":[
					{"url":"https://example.com/malformed"},
					{"type":"web_search_result","url":"https://example.com/kept"}
				]}
			]}`,
		},
		{
			name: "citation",
			raw: `{"id":"msg_1","model":"m","stop_reason":"end_turn","content":[
				{"type":"text","text":"answer","citations":[
					{"url":"https://example.com/malformed"},
					{"type":"web_search_result_location","url":"https://example.com/kept"}
				]}
			]}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var changes []compat.Change
			_, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, nil, []byte(test.raw), "ex", &changes)
			var backendErr canonical.BackendError
			if !errors.As(err, &backendErr) {
				t.Fatalf("error = %v, want backend error", err)
			}
			if len(changes) != 0 {
				t.Fatalf("changes = %#v, want no successful changes", changes)
			}
		})
	}
}

func TestDecodeResponseBufferedClassifiesMalformedWebSearchProviderDataAsBackend(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "missing call id",
			raw:  `{"id":"msg_1","model":"m","stop_reason":"end_turn","content":[{"type":"server_tool_use","name":"web_search","input":{"query":"x"}}]}`,
		},
		{
			name: "malformed action",
			raw:  `{"id":"msg_1","model":"m","stop_reason":"end_turn","content":[{"type":"server_tool_use","id":"s","name":"web_search","input":{"query":""}}]}`,
		},
		{
			name: "missing result id",
			raw:  `{"id":"msg_1","model":"m","stop_reason":"end_turn","content":[{"type":"web_search_tool_result","content":[{"type":"web_search_result","url":"https://example.com"}]}]}`,
		},
		{
			name: "malformed result content",
			raw:  `{"id":"msg_1","model":"m","stop_reason":"end_turn","content":[{"type":"web_search_tool_result","tool_use_id":"s","content":"bad"}]}`,
		},
		{
			name: "invalid result URL",
			raw:  `{"id":"msg_1","model":"m","stop_reason":"end_turn","content":[{"type":"web_search_tool_result","tool_use_id":"s","content":[{"type":"web_search_result","url":"not-a-url"}]}]}`,
		},
		{
			name: "invalid citation URL",
			raw:  `{"id":"msg_1","model":"m","stop_reason":"end_turn","content":[{"type":"text","text":"answer","citations":[{"type":"web_search_result_location","url":"not-a-url"}]}]}`,
		},
		{
			name: "invalid citation range",
			raw:  `{"id":"msg_1","model":"m","stop_reason":"end_turn","content":[{"type":"text","text":"answer","citations":[{"type":"web_search_result_location","url":"https://example.com","start_char_index":4,"end_char_index":8}]}]}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, nil, []byte(test.raw), "ex", nil)
			var backendErr canonical.BackendError
			if !errors.As(err, &backendErr) {
				t.Fatalf("error = %T %v, want backend error", err, err)
			}
		})
	}
}

func TestMessagesWebSearchCallerMalformationRemainsBadRequest(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "missing result child type",
			run: func() error {
				_, err := decodeMessagesWebSearchResult(
					"s",
					json.RawMessage(`[{"url":"https://example.com"}]`),
					false,
					messagesProjectionEvidence{feature: canonical.RequestItemsKind},
				)
				return err
			},
		},
		{
			name: "invalid citation URL",
			run: func() error {
				_, err := decodeMessagesCitedText(
					"answer",
					[]messagesCitationDTO{{Type: "web_search_result_location", URL: "not-a-url"}},
					messagesProjectionEvidence{feature: canonical.RequestItemsKind},
				)
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var canonicalErr canonical.Error
			if err := test.run(); !errors.As(err, &canonicalErr) || canonicalErr.Code != canonical.ErrorCodeBadRequest {
				t.Fatalf("error = %T %v, want BAD_REQUEST", err, err)
			}
		})
	}
}

func TestDecodeResponseBufferedRejectsAllErasedWebSearchResult(t *testing.T) {
	raw := []byte(`{"id":"msg_1","model":"m","stop_reason":"end_turn","content":[
		{"type":"server_tool_use","id":"s","name":"web_search","input":{"query":"x"}},
		{"type":"web_search_tool_result","tool_use_id":"s","content":[{"type":"future_result"}]}
	]}`)
	if _, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, nil, raw, "ex", nil); err == nil ||
		!strings.Contains(err.Error(), "backend error from messages") {
		t.Fatalf("error = %v, want backend-origin all-erased result", err)
	}
}

func TestDecodeResponseBufferedPreservesWebSearchFailure(t *testing.T) {
	raw := []byte(`{"id":"msg_1","model":"m","stop_reason":"end_turn","content":[{"type":"server_tool_use","id":"s","name":"web_search","input":{"query":"x"}},{"type":"web_search_tool_result","tool_use_id":"s","content":{"type":"web_search_tool_result_error","error_code":"unavailable"}}]}`)
	reader, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, nil, raw, "ex", nil)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Items()) < 2 {
		t.Fatalf("response items = %#v, want call and result", response.Items())
	}
	result, ok := response.Items()[1].ToolResult()
	if !ok {
		t.Fatal("web-search failure did not decode as a tool result")
	}
	search, ok := result.WebSearch()
	if failure, failed := search.Failure(); !ok || !failed || failure != "unavailable" {
		t.Fatalf("search failure = %q, %v, web search = %v", failure, failed, ok)
	}
}

func TestEncodeMessagesWebSearchFailureUsesObjectContent(t *testing.T) {
	failure, err := canonical.NewWebSearchFailureResult("unavailable")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := encodeMessagesWebSearchResult(failure)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"type":"web_search_tool_result_error","error_code":"unavailable"}` {
		t.Fatalf("content = %s", raw)
	}
}

func TestDecodeResponseStreamSkipsUnknownBlockUntilStopAndPreservesKnownSibling(t *testing.T) {
	raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"server_tool_use\",\"id\":\"s\",\"name\":\"code_execution\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"future_delta\",\"text\":\"ignored\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: response.future\ndata: {\"type\":\"response.future\",\"value\":\"ignored\"}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"kept\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	reader := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Items()) < 1 {
		t.Fatal("response has no surviving items")
	}
	message, ok := response.Items()[0].Message()
	if !ok {
		t.Fatalf("response items = %#v, want message", response.Items())
	}
	text, _ := message.Content()[0].Text()
	if text.Text() != "kept" {
		t.Fatalf("message text = %q, want kept", text.Text())
	}
}

func TestDecodeResponseStreamPreservesWebSearchLifecycle(t *testing.T) {
	raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"server_tool_use\",\"id\":\"s\",\"name\":\"web_search\",\"input\":{}}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"query\\\":\\\"x\\\"}\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"web_search_tool_result\",\"tool_use_id\":\"s\",\"content\":[{\"type\":\"web_search_result\",\"url\":\"https://example.com/x\"}]}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	reader := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Items()) != 2 {
		t.Fatalf("items=%d want=2", len(response.Items()))
	}
}

func TestDecodeResponseStreamClassifiesMalformedWebSearchSourceAsBackend(t *testing.T) {
	raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"web_search_tool_result\",\"tool_use_id\":\"s\",\"content\":[{\"type\":\"web_search_result\",\"url\":\"not-a-url\"}]}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"
	reader := decodeResponseStream(
		canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))},
		"ex",
		nil,
	)
	for {
		_, err := reader.Next(context.Background())
		if err == nil {
			continue
		}
		var backendErr canonical.BackendError
		if !errors.As(err, &backendErr) {
			t.Fatalf("error = %T %v, want backend error", err, err)
		}
		return
	}
}

func TestDecodeResponseStreamPreservesWebSearchFailure(t *testing.T) {
	raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"server_tool_use\",\"id\":\"s\",\"name\":\"web_search\",\"input\":{\"query\":\"x\"}}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"web_search_tool_result\",\"tool_use_id\":\"s\",\"content\":{\"type\":\"web_search_tool_result_error\",\"error_code\":\"too_many_requests\"}}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	reader := decodeResponseStream(canonical.CanonicalRequest{}, nil, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{SwobuID: "resp_test"}), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Items()) < 2 {
		t.Fatalf("response items = %#v, want call and result", response.Items())
	}
	result, ok := response.Items()[1].ToolResult()
	if !ok {
		t.Fatal("web-search failure did not decode as a tool result")
	}
	search, ok := result.WebSearch()
	if failure, failed := search.Failure(); !ok || !failed || failure != "too_many_requests" {
		t.Fatalf("search failure = %q, %v, web search = %v", failure, failed, ok)
	}
}
