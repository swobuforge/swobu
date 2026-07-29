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
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

func TestMessagesAssistantImageFailsAsClientOutputContract(t *testing.T) {
	image, _ := canonical.NewURLImage("https://example.test/output.png", canonical.Unspecified[canonical.ImageDetail]())
	message, err := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{
		canonical.NewImageMessagePart(image),
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: "response-image"},
		"model",
		[]canonical.CanonicalItem{message},
		canonical.Completed("stop"),
		canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (ResponseDocumentEncoder{}).EncodeResponseDocument(canonical.CanonicalRequest{}, response)
	var backendErr canonical.BackendError
	if !errors.As(err, &backendErr) {
		t.Fatalf("image output error = %T %v, want BackendError", err, err)
	}
}

func TestMessagesStreamedAssistantImageFailsAsClientOutputContract(t *testing.T) {
	response := messagesAssistantImageResponse(t)
	events := canonical.SynthesizeResponseEnvelopeEvents(
		"exchange-image", response.Response(), response.Model(), response.Items(), response.Completion(), response.Usage(),
	)
	encoded, err := (ResponseStreamEncoder{}).EncodeResponseStream(
		context.Background(),
		canonical.CanonicalRequest{},
		canonical.NewSliceEventReader(events),
		delivery.StreamingDelivery(delivery.FramingSSE),
	)
	if err == nil {
		_, err = io.Copy(io.Discard, encoded.Stream.Body)
	}
	var backendErr canonical.BackendError
	if !errors.As(err, &backendErr) {
		t.Fatalf("streamed image output error = %T %v, want BackendError", err, err)
	}
}

func messagesAssistantImageResponse(t *testing.T) canonical.CanonicalResponse {
	t.Helper()
	image, _ := canonical.NewURLImage("https://example.test/output.png", canonical.Unspecified[canonical.ImageDetail]())
	message, err := canonical.NewMessageItem(canonical.MessageRoleAssistant, []canonical.MessagePart{
		canonical.NewImageMessagePart(image),
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := canonical.NewCanonicalResponse(
		canonical.ResponseRef{SwobuID: "response-image"},
		"model",
		[]canonical.CanonicalItem{message},
		canonical.Completed("stop"),
		canonical.NewUnknownTokenUsage(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func TestEncodeMessagesImages_PreservesDirectURLAndNestedToolResultImages(t *testing.T) {
	urlImage, _ := canonical.NewURLImage("https://example.test/direct.png", canonical.Unspecified[canonical.ImageDetail]())
	inlineImage, _ := canonical.NewInlineImage(canonical.ImageMediaPNG, imageTestPNG(), canonical.Unspecified[canonical.ImageDetail]())
	message, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{
		canonical.NewImageMessagePart(urlImage), canonical.NewImageMessagePart(inlineImage),
	})
	callID, _ := canonical.NewToolCallID("call_image")
	result, _ := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{
		canonical.NewTextToolResultPart("before"),
		canonical.NewImageToolResultPart(urlImage),
		canonical.NewImageToolResultPart(inlineImage),
	}, false)
	req := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{message, result}})

	doc, err := EncodeCarrierWithDecisions(req, delivery.BufferedDelivery(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Messages []struct {
			Content []struct {
				Type    string            `json:"type"`
				Source  map[string]string `json:"source"`
				Content []struct {
					Type   string            `json:"type"`
					Text   string            `json:"text"`
					Source map[string]string `json:"source"`
				} `json:"content"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(doc.Raw, &payload); err != nil {
		t.Fatal(err)
	}
	if got := payload.Messages[0].Content[0].Source; got["type"] != "url" || got["url"] != "https://example.test/direct.png" {
		t.Fatalf("direct image source = %#v", got)
	}
	if got := payload.Messages[0].Content[1].Source; got["type"] != "base64" {
		t.Fatalf("direct inline image source = %#v", got)
	}
	nested := payload.Messages[0].Content[2].Content
	if len(nested) != 3 || nested[0].Type != "text" || nested[1].Source["type"] != "url" || nested[2].Source["type"] != "base64" {
		t.Fatalf("nested tool-result content = %#v", nested)
	}
}

func TestDecodeMessagesUserImages_PreservesURLAndInlineSources(t *testing.T) {
	raw := []byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"https://example.test/direct.png"}},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw0KGgo="}}]}]}`)
	result, err := (ClientRequestDecoder{ImageLimits: shared.ImageDecodeLimitPolicy{MaxInlineBytes: 8}}).DecodeClientRequest(carrier.Document{Family: protocolkind.Messages, Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	message, _ := result.Request.Request.Items()[0].Message()
	parts := message.Content()
	first, _ := parts[0].Image()
	second, _ := parts[1].Image()
	if _, ok := first.Source().URL(); !ok {
		t.Fatal("Messages URL image did not remain a URL source")
	}
	if _, ok := second.Source().Inline(); !ok {
		t.Fatal("Messages inline image did not remain an inline source")
	}
}

func TestDecodeMessagesImages_PreservesNestedToolResultOrderAndError(t *testing.T) {
	raw := []byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_image","is_error":true,"content":[{"type":"text","text":"before"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw0KGgo="}},{"type":"text","text":"after"}]},{"type":"text","text":"next"}]}]}`)
	result, err := (ClientRequestDecoder{ImageLimits: shared.ImageDecodeLimitPolicy{MaxInlineBytes: 8}}).DecodeClientRequest(carrier.Document{Family: protocolkind.Messages, Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	items := result.Request.Request.Items()
	if len(items) != 2 {
		t.Fatalf("items len = %d, want tool result and message", len(items))
	}
	toolResult, ok := items[0].ToolResult()
	if !ok || !toolResult.IsError() {
		t.Fatalf("tool result = %#v, want error result", toolResult)
	}
	parts := toolResult.Content()
	if len(parts) != 3 || parts[0].Kind() != canonical.PartKindText || parts[1].Kind() != canonical.PartKindImage || parts[2].Kind() != canonical.PartKindText {
		t.Fatalf("nested parts = %#v", parts)
	}
}

func TestEncodeMessagesImageDetailOmitsWithDecision(t *testing.T) {
	image, _ := canonical.NewURLImage("https://example.test/detail.png", canonical.Specify(canonical.ImageDetailHigh))
	message, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewImageMessagePart(image)})
	req := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{message}})

	sink := &recordingDecisionSink{}
	if _, err := EncodeCarrierWithDecisions(req, delivery.BufferedDelivery(), sink, "ex"); err != nil {
		t.Fatalf("Messages lowering failed: %v", err)
	}
	if len(sink.effects) != 1 || sink.effects[0].Feature != compat.RequestItemsMessageImageDetail || sink.effects[0].Outcome != compat.Approx {
		t.Fatalf("decisions = %#v", sink.effects)
	}
}

func TestEncodeMessagesImages_URLCarrierUsesURLBlock(t *testing.T) {
	image, _ := canonical.NewURLImage("https://example.test/bedrock.png", canonical.Unspecified[canonical.ImageDetail]())
	message, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewImageMessagePart(image)})
	req := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{message}})
	doc, err := EncodeCarrierWithDecisions(req, delivery.BufferedDelivery(), nil, "ex")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc.RawBytes()), `"type":"url"`) {
		t.Fatalf("Messages URL image = %s", doc.RawBytes())
	}
}

func TestEncodeMessagesToolResult_MultipleTextPartsRemainAnArray(t *testing.T) {
	callID, _ := canonical.NewToolCallID("call_texts")
	result, _ := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{
		canonical.NewTextToolResultPart("one"),
		canonical.NewTextToolResultPart("two"),
	}, false)
	req := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{result}})
	doc, err := EncodeCarrierWithDecisions(req, delivery.BufferedDelivery(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Messages []struct {
			Content []struct {
				Content any `json:"content"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(doc.Raw, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload.Messages[0].Content[0].Content.([]any); !ok {
		t.Fatalf("multiple text parts collapsed to %#v", payload.Messages[0].Content[0].Content)
	}
}

func TestDecodeMessagesImageRejectsProviderFileSource(t *testing.T) {
	raw := []byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"image","source":{"type":"file","file_id":"file_secret"}}]}]}`)
	_, err := (ClientRequestDecoder{ImageLimits: shared.ImageDecodeLimitPolicy{MaxInlineBytes: 8}}).DecodeClientRequest(carrier.Document{Family: protocolkind.Messages, Raw: raw})
	if err == nil {
		t.Fatal("Messages provider file image source was accepted")
	}
}

func imageTestPNG() []byte {
	return []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
}
