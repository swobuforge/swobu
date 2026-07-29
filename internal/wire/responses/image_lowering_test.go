package responses

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

func TestResponsesAssistantImageFailsAsClientOutputContract(t *testing.T) {
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

func TestResponsesStreamedAssistantImageFailsAsClientOutputContract(t *testing.T) {
	response := responsesAssistantImageResponse(t)
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

func responsesAssistantImageResponse(t *testing.T) canonical.CanonicalResponse {
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

func TestEncodeResponsesImages_PreservesUserSourcesAndToolResultPlacement(t *testing.T) {
	urlImage, err := canonical.NewURLImage("https://example.test/user.png", canonical.Specify(canonical.ImageDetailOriginal))
	if err != nil {
		t.Fatal(err)
	}
	inlineImage, err := canonical.NewInlineImage(canonical.ImageMediaPNG, imageTestPNG(), canonical.Unspecified[canonical.ImageDetail]())
	if err != nil {
		t.Fatal(err)
	}
	message, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{
		canonical.NewImageMessagePart(urlImage),
		canonical.NewImageMessagePart(inlineImage),
	})
	if err != nil {
		t.Fatal(err)
	}
	callID, _ := canonical.NewToolCallID("call_image")
	result, err := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{
		canonical.NewTextToolResultPart("before"),
		canonical.NewImageToolResultPart(urlImage),
		canonical.NewImageToolResultPart(inlineImage),
		canonical.NewTextToolResultPart("after"),
	}, false)
	if err != nil {
		t.Fatal(err)
	}

	doc, err := EncodeCarrier(canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{message, result},
	}), delivery.BufferedDelivery())
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Input []struct {
			Type    string           `json:"type"`
			Content []map[string]any `json:"content"`
			Output  []map[string]any `json:"output"`
		} `json:"input"`
	}
	if err := json.Unmarshal(doc.Raw, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Input) != 2 {
		t.Fatalf("input len = %d, want 2", len(payload.Input))
	}
	if got := payload.Input[0].Content[0]; got["type"] != "input_image" || got["image_url"] != "https://example.test/user.png" || got["detail"] != "original" {
		t.Fatalf("user image = %#v", got)
	}
	if rawURL, _ := payload.Input[0].Content[1]["image_url"].(string); !strings.HasPrefix(rawURL, "data:image/png;base64,") {
		t.Fatalf("user inline image_url = %#v", payload.Input[0].Content[1]["image_url"])
	}
	output := payload.Input[1].Output
	if len(output) != 4 || output[0]["type"] != "input_text" || output[1]["image_url"] != "https://example.test/user.png" || output[2]["type"] != "input_image" || output[3]["type"] != "input_text" {
		t.Fatalf("tool-result output = %#v", output)
	}
	if rawURL, _ := output[2]["image_url"].(string); !strings.HasPrefix(rawURL, "data:image/png;base64,") {
		t.Fatalf("tool-result inline image_url = %#v", output[2]["image_url"])
	}
}

func TestDecodeResponsesImages_PreservesToolResultArrayOrder(t *testing.T) {
	raw := []byte(`{"model":"m","input":[{"type":"function_call_output","call_id":"call_image","output":[{"type":"input_text","text":"before"},{"type":"input_image","image_url":"data:image/png;base64,iVBORw0KGgo=","detail":"original"},{"type":"input_text","text":"after"}]}]}`)
	result, err := (ClientRequestDecoder{ImageLimits: shared.ImageDecodeLimitPolicy{MaxInlineBytes: 8}}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	items := result.Request.Request.Items()
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	toolResult, ok := items[0].ToolResult()
	if !ok {
		t.Fatal("decoded item is not a tool result")
	}
	parts := toolResult.Content()
	if len(parts) != 3 || parts[0].Kind() != canonical.PartKindText || parts[1].Kind() != canonical.PartKindImage || parts[2].Kind() != canonical.PartKindText {
		t.Fatalf("tool-result parts = %#v", parts)
	}
	image, _ := parts[1].Image()
	detail, specified := image.Detail().Get()
	if !specified || detail != canonical.ImageDetailOriginal {
		t.Fatalf("detail = %q specified=%v", detail, specified)
	}
}

func TestEncodeResponsesToolResult_MultipleTextPartsRemainAnArray(t *testing.T) {
	callID, _ := canonical.NewToolCallID("call_texts")
	result, _ := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{
		canonical.NewTextToolResultPart("one"),
		canonical.NewTextToolResultPart("two"),
	}, false)
	doc, err := EncodeCarrier(canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{result},
	}), delivery.BufferedDelivery())
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Input []struct {
			Output any `json:"output"`
		} `json:"input"`
	}
	if err := json.Unmarshal(doc.Raw, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload.Input[0].Output.([]any); !ok {
		t.Fatalf("multiple text parts collapsed to %#v", payload.Input[0].Output)
	}
}

func TestDecodeResponsesToolResultRejectsProviderFileID(t *testing.T) {
	raw := []byte(`{"model":"m","input":[{"type":"function_call_output","call_id":"call_image","output":[{"type":"input_image","file_id":"file_secret"}]}]}`)
	_, err := (ClientRequestDecoder{ImageLimits: shared.ImageDecodeLimitPolicy{MaxInlineBytes: 8}}).DecodeClientRequest(carrier.Document{Family: protocolkind.Responses, Raw: raw})
	if err == nil {
		t.Fatal("Responses tool-result provider file ID was accepted")
	}
}

func imageTestPNG() []byte {
	return []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
}
