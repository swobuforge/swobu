package responses

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/session"
	"github.com/swobuforge/swobu/internal/testkit/canonicaltest"
)

func TestCheckpointToDifferentResponsesTargetReplaysOneCanonicalGraph(t *testing.T) {
	for _, streamed := range []bool{false, true} {
		name := "buffered turn one"
		if streamed {
			name = "streamed turn one"
		}
		t.Run(name, func(t *testing.T) {
			priorRequest, media := replayFixturePriorRequest(t)
			response := decodeReplayFixtureResponse(t, priorRequest, streamed)
			store := session.NewMemoryStore()
			if err := store.Put(context.Background(), "dev", session.Checkpoint{
				Request: priorRequest, Response: response, ResolvedMedia: media,
			}); err != nil {
				t.Fatal(err)
			}
			checkpoint, found, err := store.Get(context.Background(), "dev", "swobu_turn_1")
			if err != nil || !found {
				t.Fatalf("latest checkpoint = (%t, %v)", found, err)
			}

			current := canonical.NewCanonicalRequest(canonical.RequestParams{
				Model: canonical.Specify("m"),
				Items: []canonical.CanonicalItem{
					canonicaltest.Message(t, canonical.MessageRoleUser, "turn two"),
				},
				PreviousResponse: &canonical.ResponseRef{SwobuID: "swobu_turn_1"},
			})
			resolved, err := session.Resume(current, checkpoint)
			if err != nil {
				t.Fatal(err)
			}
			asset, ok := resolved.ResolvedMedia.Resolve(
				canonical.RequestPartRef{Item: 1, Part: 1},
				"https://example.test/image.png?version=one",
			)
			if !ok || string(asset.Bytes()) != "durable-image" {
				t.Fatalf("resolved media was not retained: %#v", asset)
			}

			differentTarget := provider.NewTargetSnapshot(
				"responses-b", "openai", "https://example.test", "cred",
				protocolkind.Responses, "m", "responses",
			)
			differentTarget.TargetVersion = 2
			stateless := resolved.ForTarget(differentTarget)
			if _, hasPrevious := stateless.PreviousResponse(); hasPrevious {
				t.Fatal("different target retained previous_response_id")
			}
			document, err := EncodeCarrierWithDecisions(
				EncodeInput{Request: stateless}, delivery.BufferedDelivery(), nil, "", EncodeOptions{},
			)
			if err != nil {
				t.Fatal(err)
			}
			assertReplayFixtureWireOrder(t, document.RawBytes())
		})
	}
}

func replayFixturePriorRequest(t *testing.T) (canonical.CanonicalRequest, session.ResolvedMedia) {
	t.Helper()
	image, err := canonical.NewURLImage(
		"https://example.test/image.png?version=one",
		canonical.Unspecified[canonical.ImageDetail](),
	)
	if err != nil {
		t.Fatal(err)
	}
	message, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{
		canonical.NewTextMessagePart("turn one"),
		canonical.NewImageMessagePart(image),
	})
	if err != nil {
		t.Fatal(err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"),
		Items: []canonical.CanonicalItem{canonicaltest.ToolDeclarations(t,
			canonicaltest.MustFunctionTool(
				canonicaltest.MustRequestToolKey(canonical.ToolKindFunction, "search"),
				"", canonicaltest.Schema(t, `{"type":"object"}`), canonical.Unspecified[bool](),
			),
			canonical.NewWebSearchDeclaration(),
		), message},
	})
	media, err := (session.ResolvedMedia{}).Bind(
		canonical.RequestPartRef{Item: 1, Part: 1},
		"https://example.test/image.png?version=one",
		canonical.ImageMediaPNG, []byte("durable-image"),
	)
	if err != nil {
		t.Fatal(err)
	}
	return request, media
}

func decodeReplayFixtureResponse(t *testing.T, request canonical.CanonicalRequest, streamed bool) canonical.CanonicalResponse {
	t.Helper()
	output := `[{"type":"reasoning","id":"rs_1","status":"completed","encrypted_content":"cipher"},{"type":"message","id":"msg_1","status":"completed","role":"assistant","content":[{"type":"output_text","text":"use the tool"}]},{"type":"function_call","id":"fc_1","status":"completed","call_id":"call_1","name":"search","arguments":"{}"},{"type":"web_search_call","id":"ws_lifecycle","status":"completed","action":{"type":"search","queries":["deadline"],"sources":[{"type":"url","url":"https://example.test/rules","title":"Rules"}]}}]`
	var reader canonical.ResponseStream
	if streamed {
		raw := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"provider_turn_1\",\"model\":\"m\",\"status\":\"in_progress\"}}\n\n" +
			"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"id\":\"rs_1\",\"type\":\"reasoning\",\"status\":\"in_progress\",\"summary\":[]}}\n\n" +
			"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"id\":\"rs_1\",\"type\":\"reasoning\",\"status\":\"completed\",\"summary\":[],\"encrypted_content\":\"cipher\"}}\n\n" +
			"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":1,\"content_index\":0,\"delta\":\"use the tool\"}\n\n" +
			"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"id\":\"msg_1\",\"type\":\"message\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"use the tool\"}]}}\n\n" +
			"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":2,\"item\":{\"id\":\"fc_1\",\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"search\"}}\n\n" +
			"event: response.function_call_arguments.delta\ndata: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":2,\"item_id\":\"fc_1\",\"call_id\":\"call_1\",\"name\":\"search\",\"delta\":\"{}\"}\n\n" +
			"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":2,\"item\":{\"id\":\"fc_1\",\"type\":\"function_call\",\"call_id\":\"call_1\",\"name\":\"search\",\"arguments\":\"{}\"}}\n\n" +
			"event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":3,\"item\":{\"id\":\"ws_lifecycle\",\"type\":\"web_search_call\",\"status\":\"in_progress\"}}\n\n" +
			"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":3,\"item\":{\"id\":\"ws_lifecycle\",\"type\":\"web_search_call\",\"status\":\"completed\",\"action\":{\"type\":\"search\",\"queries\":[\"deadline\"],\"sources\":[{\"type\":\"url\",\"url\":\"https://example.test/rules\",\"title\":\"Rules\"}]}}}\n\n" +
			"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"provider_turn_1\",\"model\":\"m\",\"status\":\"completed\",\"output\":[]}}\n\n"
		reader = decodeResponseStream(
			request,
			carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))},
			"ex", nil,
		)
	} else {
		raw := []byte(`{"id":"provider_turn_1","model":"m","status":"completed","output":` + output + `}`)
		var err error
		reader, err = decodeResponseBuffered(context.Background(), request, raw, "ex", nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	bound := canonical.NewBoundResponseIdentityStream(reader, canonical.ResponseBinding{
		SwobuID: "swobu_turn_1", TargetID: "responses-a", TargetVersion: 1,
	})
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewValidatedResponseStream(bound), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	return *response
}

func assertReplayFixtureWireOrder(t *testing.T, raw []byte) {
	t.Helper()
	var payload struct {
		PreviousResponseID string            `json:"previous_response_id"`
		Input              []json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.PreviousResponseID != "" || len(payload.Input) != 6 {
		t.Fatalf("stateless payload shape = %#v: %s", payload, raw)
	}
	wantTypes := []string{"message", "reasoning", "message", "function_call", "web_search_call", "message"}
	for index, want := range wantTypes {
		var header struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(payload.Input[index], &header); err != nil || header.Type != want {
			t.Fatalf("input[%d] = %s, want type %q", index, payload.Input[index], want)
		}
	}
	for _, token := range []string{
		`"encrypted_content":"cipher"`,
		`"call_id":"call_1"`,
		`"id":"ws_lifecycle"`,
		`"query":"deadline"`,
		`"url":"https://example.test/rules"`,
		`turn two`,
	} {
		if !strings.Contains(string(raw), token) {
			t.Fatalf("stateless replay missing %q: %s", token, raw)
		}
	}
}
