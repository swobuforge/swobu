package wire_test

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/wire"
	"github.com/swobuforge/swobu/internal/wire/messages"
	"github.com/swobuforge/swobu/internal/wire/responses"
)

func TestUnicodeCitationRoundTripsAcrossResponsesAndMessages(t *testing.T) {
	tests := []struct {
		name   string
		decode func(t *testing.T) canonical.CanonicalResponse
		encode func(canonical.CanonicalResponse) (wire.ClientDocumentResult, error)
		replay func(t *testing.T, document carrier.Document) canonical.CanonicalResponse
	}{
		{
			name: "Responses to Messages",
			decode: func(t *testing.T) canonical.CanonicalResponse {
				raw := []byte(`{"id":"resp_1","model":"m","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"A£😀B","annotations":[{"type":"url_citation","url":"https://example.com/source","title":"Source","start_index":1,"end_index":2}]}]}]}`)
				decoded, err := (responses.ProviderDocumentDecoder{}).DecodeProviderDocument(context.Background(), canonical.CanonicalRequest{}, nil, carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}), "exchange")
				if err != nil {
					t.Fatal(err)
				}
				return projectBoundResponse(t, decoded)
			},
			encode: func(response canonical.CanonicalResponse) (wire.ClientDocumentResult, error) {
				return (messages.ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
			},
			replay: func(t *testing.T, document carrier.Document) canonical.CanonicalResponse {
				decoded, err := (messages.ProviderDocumentDecoder{}).DecodeProviderDocument(context.Background(), canonical.CanonicalRequest{}, nil, document, "exchange")
				if err != nil {
					t.Fatal(err)
				}
				return projectBoundResponse(t, decoded)
			},
		},
		{
			name: "Messages to Responses",
			decode: func(t *testing.T) canonical.CanonicalResponse {
				raw := []byte(`{"id":"msg_1","model":"m","role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"A£😀B","citations":[{"type":"web_search_result_location","url":"https://example.com/source","title":"Source","start_char_index":1,"end_char_index":3}]}]}`)
				decoded, err := (messages.ProviderDocumentDecoder{}).DecodeProviderDocument(context.Background(), canonical.CanonicalRequest{}, nil, carrier.NewDocument(protocolkind.Messages, "application/json", nil, raw, carrier.Meta{}), "exchange")
				if err != nil {
					t.Fatal(err)
				}
				return projectBoundResponse(t, decoded)
			},
			encode: func(response canonical.CanonicalResponse) (wire.ClientDocumentResult, error) {
				return (responses.ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
			},
			replay: func(t *testing.T, document carrier.Document) canonical.CanonicalResponse {
				decoded, err := (responses.ProviderDocumentDecoder{}).DecodeProviderDocument(context.Background(), canonical.CanonicalRequest{}, nil, document, "exchange")
				if err != nil {
					t.Fatal(err)
				}
				return projectBoundResponse(t, decoded)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			initial := test.decode(t)
			assertUnicodeCitation(t, initial)
			encoded, err := test.encode(initial)
			if err != nil {
				t.Fatal(err)
			}
			assertUnicodeCitation(t, test.replay(t, encoded.Document))
		})
	}
}

func projectBoundResponse(t *testing.T, decoded wire.ProviderDecodeResult) canonical.CanonicalResponse {
	t.Helper()
	bound := canonical.NewBoundResponseIdentityStream(decoded.Stream, canonical.ResponseBinding{SwobuID: "resp_swobu", TargetID: "target", TargetVersion: 1})
	envelope, err := canonical.ReadClosedEnvelope(context.Background(), bound, canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := envelope.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	if response == nil {
		t.Fatal("projected response is nil")
	}
	return *response
}

func assertUnicodeCitation(t *testing.T, response canonical.CanonicalResponse) {
	t.Helper()
	items := response.Items()
	if len(items) != 1 {
		t.Fatalf("items = %d, want one message", len(items))
	}
	message, ok := items[0].Message()
	if !ok || len(message.Content()) != 1 {
		t.Fatalf("message = %#v", items[0])
	}
	part := message.Content()[0]
	text, ok := part.Text()
	if !ok || text.Text() != "A£😀B" {
		t.Fatalf("text = %#v", part)
	}
	citations := part.Citations()
	if len(citations) != 1 {
		t.Fatalf("citations = %#v", citations)
	}
	start, hasStart := citations[0].Start.Get()
	end, hasEnd := citations[0].End.Get()
	if !hasStart || !hasEnd || start != 1 || end != 7 || text.Text()[start:end] != "£😀" {
		t.Fatalf("citation span = [%d,%d) specified=(%t,%t)", start, end, hasStart, hasEnd)
	}
}
