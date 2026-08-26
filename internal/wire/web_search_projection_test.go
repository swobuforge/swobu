package wire_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/wire/messages"
	"github.com/swobuforge/swobu/internal/wire/responses"
)

func TestResponsesUnrepresentableLifecycleProjectsAtomicallyToMessages(t *testing.T) {
	actions := map[string]string{
		"open page":        `{"type":"open_page","url":"https://example.com/source#section"}`,
		"find in page":     `{"type":"find_in_page","url":"https://example.com/source#section","pattern":"needle"}`,
		"queryless search": `{"type":"search"}`,
		"multi query":      `{"type":"search","queries":["one","two"]}`,
	}
	for name, action := range actions {
		t.Run(name, func(t *testing.T) {
			action = action[:len(action)-1] + `,"sources":[{"type":"url","url":"https://example.com/source#section","title":"Source"}]}`
			raw := []byte(`{"id":"resp_provider","model":"model","status":"completed","output":[{"type":"web_search_call","id":"ws_original","status":"completed","action":` + action + `},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer","annotations":[{"type":"url_citation","url":"https://example.com/source#section","title":"Source"}]}]}]}`)
			response := decodeResponsesOutput(t, raw)
			encoded, err := (messages.ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
			if err != nil {
				t.Fatal(err)
			}
			output := encoded.Document.RawBytes()
			if bytes.Contains(output, []byte("ws_original")) || bytes.Contains(output, []byte("web_search_tool_result")) {
				t.Fatalf("Responses lifecycle leaked into Messages: %s", output)
			}
			if !bytes.Contains(output, []byte(`"text":"answer"`)) || !bytes.Contains(output, []byte(`"url":"https://example.com/source#section"`)) {
				t.Fatalf("text or citation was lost: %s", output)
			}
			if len(encoded.Changes) != 1 ||
				encoded.Changes[0].Capability != canonical.ResponseItemsKind ||
				encoded.Changes[0].Kind != compat.Omission {
				t.Fatalf("changes = %#v", encoded.Changes)
			}
			item, ok := encoded.Changes[0].Occurrence.ResponseItem()
			if !ok || item != 0 {
				t.Fatalf("change occurrence = %#v, want response item 0", encoded.Changes[0].Occurrence)
			}
		})
	}
}

func TestMessagesLifecycleProjectsToResponsesWithOriginalIdentity(t *testing.T) {
	raw := []byte(`{"id":"msg_1","model":"model","stop_reason":"end_turn","content":[{"type":"server_tool_use","id":"ws_original","name":"web_search","input":{"query":"one"}},{"type":"web_search_tool_result","tool_use_id":"ws_original","content":[{"type":"web_search_result","url":"https://example.com/source","title":"Source"}]},{"type":"text","text":"answer","citations":[{"type":"web_search_result_location","url":"https://example.com/source","title":"Source"}]}]}`)
	decoded, err := (messages.ProviderDocumentDecoder{}).DecodeProviderDocument(context.Background(), canonical.CanonicalRequest{}, nil, carrier.NewDocument(protocolkind.Messages, "application/json", nil, raw, carrier.Meta{}), "exchange")
	if err != nil {
		t.Fatal(err)
	}
	bound := canonical.NewBoundResponseIdentityStream(decoded.Stream, canonical.ResponseBinding{SwobuID: "resp_test"})
	closed, err := canonical.ReadClosedEnvelope(context.Background(), bound, canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := (responses.ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, *response)
	if err != nil {
		t.Fatal(err)
	}
	output := encoded.Document.RawBytes()
	for _, value := range [][]byte{[]byte(`"id":"ws_original"`), []byte(`"query":"one"`), []byte(`"url":"https://example.com/source"`), []byte(`"text":"answer"`)} {
		if !bytes.Contains(output, value) {
			t.Fatalf("Responses projection lacks %s: %s", value, output)
		}
	}
}

func decodeResponsesOutput(t *testing.T, raw []byte) canonical.CanonicalResponse {
	t.Helper()
	decoded, err := (responses.ProviderDocumentDecoder{}).DecodeProviderDocument(
		context.Background(), canonical.CanonicalRequest{}, nil, carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}), "exchange",
	)
	if err != nil {
		t.Fatal(err)
	}
	bound := canonical.NewBoundResponseIdentityStream(decoded.Stream, canonical.ResponseBinding{SwobuID: "resp_test"})
	closed, err := canonical.ReadClosedEnvelope(context.Background(), bound, canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	return *response
}
