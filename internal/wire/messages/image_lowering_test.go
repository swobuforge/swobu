package messages

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

func TestDecodeMessagesProviderAssistantImageBesideTextDropsImage(t *testing.T) {
	raw := []byte(`{"id":"msg_1","model":"m","stop_reason":"end_turn","content":[{"type":"text","text":"Here is the result"},{"type":"image","source":{"type":"url","url":"https://example.test/output.png"}}]}`)
	var changes []compat.Change
	stream, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, nil, raw, "ex_image", &changes)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := canonical.ReadClosedEnvelope(context.Background(), canonical.NewBoundResponseIdentityStream(stream, canonical.ResponseBinding{SwobuID: "response-image"}), canonical.EnvResponse)
	if err != nil {
		t.Fatal(err)
	}
	response, err := closed.ProjectResponse()
	if err != nil {
		t.Fatal(err)
	}
	message, ok := response.Items()[0].Message()
	if !ok || len(message.Content()) != 1 {
		t.Fatalf("surviving message = %#v", response.Items())
	}
	text, ok := message.Content()[0].Text()
	if !ok || text.Text() != "Here is the result" {
		t.Fatalf("surviving text = %q, %v", text, ok)
	}
	drops := 0
	for _, decision := range changes {
		item, occurrenceOK := decision.Occurrence.ResponseItem()
		if decision.Capability == canonical.ResponseItemsKind && decision.Kind == compat.Omission &&
			occurrenceOK && item == 1 {
			drops++
		}
	}
	if drops != 1 {
		t.Fatalf("assistant image omission changes = %d, want 1; all=%#v", drops, changes)
	}
}

func TestDecodeMessagesProviderImageOnlyFailsOutputContract(t *testing.T) {
	raw := []byte(`{"id":"msg_1","model":"m","stop_reason":"end_turn","content":[{"type":"image","source":{"type":"url","url":"https://example.test/output.png"}}]}`)
	_, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, nil, raw, "ex_image", nil)
	var backendErr canonical.BackendError
	if !errors.As(err, &backendErr) || backendErr.Message != "backend produced no usable canonical output" {
		t.Fatalf("image-only error = %T %v, want backend output-contract failure", err, err)
	}
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

	doc, err := EncodeCarrierWithChanges(req, testAttemptToolNames(req), delivery.BufferedDelivery(), nil, "")
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

	var changes []compat.Change
	if _, err := EncodeCarrierWithChanges(req, testAttemptToolNames(req), delivery.BufferedDelivery(), &changes, "ex"); err != nil {
		t.Fatalf("Messages lowering failed: %v", err)
	}
	if len(changes) != 1 || changes[0].Capability != canonical.RequestItemsMessageImageDetail || changes[0].Kind != compat.Approximation {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestEncodeMessagesImages_URLCarrierUsesURLBlock(t *testing.T) {
	image, _ := canonical.NewURLImage("https://example.test/bedrock.png", canonical.Unspecified[canonical.ImageDetail]())
	message, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewImageMessagePart(image)})
	req := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{message}})
	doc, err := EncodeCarrierWithChanges(req, testAttemptToolNames(req), delivery.BufferedDelivery(), nil, "ex")
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
	doc, err := EncodeCarrierWithChanges(req, testAttemptToolNames(req), delivery.BufferedDelivery(), nil, "")
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
