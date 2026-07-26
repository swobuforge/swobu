package responses_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/session"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
	"github.com/swobuforge/swobu/internal/wire/chatcompletions"
	"github.com/swobuforge/swobu/internal/wire/messages"
	"github.com/swobuforge/swobu/internal/wire/responses"
)

func TestAdditionalToolsCanonicalProjectionAcrossProtocolsAndCheckpoint(t *testing.T) {
	raw := []byte(`{
		"model":"m",
		"input":[
			{"type":"additional_tools","role":"developer","tools":[{"type":"function","name":"search","description":"Search docs","parameters":{"type":"object"}}]},
			{"type":"message","role":"user","content":"search"}
		]
	}`)
	decoded, err := (responses.ClientRequestDecoder{}).DecodeClientRequest(
		carrier.NewDocument(protocolkind.Responses, "application/json", nil, raw, carrier.Meta{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	responseID := canonical.NewSwobuResponseID("resp_additional_tools")
	response, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: responseID},
		"m",
		nil,
		"stop",
		canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	store := session.NewMemoryStore()
	if err := store.Put(context.Background(), "dev", session.Checkpoint{Request: decoded.Request.Request, Response: response}); err != nil {
		t.Fatal(err)
	}
	checkpoint, found, err := store.Get(context.Background(), "dev", responseID)
	if err != nil || !found {
		t.Fatalf("checkpoint = (%t, %v)", found, err)
	}
	request := checkpoint.Request
	if len(canonicaltest.Tools(request)) != 1 {
		t.Fatalf("checkpoint tools = %#v", canonicaltest.Tools(request))
	}

	responsesDocument, err := responses.EncodeCarrierWithDecisions(
		responses.EncodeInput{Request: request}, delivery.BufferedDelivery(), nil, "", responses.EncodeOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(responsesDocument.RawBytes()), `"type":"additional_tools"`) ||
		!strings.Contains(string(responsesDocument.RawBytes()), `"name":"search"`) {
		t.Fatalf("Responses projection lost ordered declaration carrier: %s", responsesDocument.RawBytes())
	}

	chatDocument, err := chatcompletions.EncodeCarrierWithDecisions(
		request, delivery.BufferedDelivery(), nil, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	assertProjectedToolCarrier(t, chatDocument.RawBytes(), "tools")

	messagesDocument, err := messages.EncodeCarrierWithDecisions(
		request, delivery.BufferedDelivery(), nil, "",
	)
	if err != nil {
		t.Fatal(err)
	}
	assertProjectedToolCarrier(t, messagesDocument.RawBytes(), "tools")
}

func assertProjectedToolCarrier(t *testing.T, raw []byte, field string) {
	t.Helper()
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	var tools []map[string]any
	if err := json.Unmarshal(payload[field], &tools); err != nil {
		t.Fatalf("%s tools: %v\n%s", field, err, raw)
	}
	projected, err := json.Marshal(tools)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || !strings.Contains(string(projected), `"name":"search"`) {
		t.Fatalf("%s tools = %#v", field, tools)
	}
}
