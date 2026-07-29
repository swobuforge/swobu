package responses

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

func TestDecodeResponsesProviderAssistantImageBesideTextDropsImage(t *testing.T) {
	raw := []byte(`{"id":"resp_1","model":"m","status":"completed","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Here is the result"},{"type":"output_image","image_url":"https://example.test/output.png"}]}]}`)
	sink := &recordingDecisionSink{}
	stream, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, raw, "ex_image", sink)
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
	for _, decision := range sink.effects {
		if decision.Feature == compat.ResponseItemsKind && decision.Outcome == compat.Drop &&
			decision.Subject == compat.Subject("wire:/output/0/content/1/type") {
			drops++
		}
	}
	if drops != 1 {
		t.Fatalf("assistant image drop decisions = %d, want 1; all=%#v", drops, sink.effects)
	}
}

func TestDecodeResponsesProviderImageOnlyFailsOutputContract(t *testing.T) {
	raw := []byte(`{"id":"resp_1","model":"m","status":"completed","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_image","image_url":"https://example.test/output.png"}]}]}`)
	_, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, raw, "ex_image", nil)
	var backendErr canonical.BackendError
	if !errors.As(err, &backendErr) || backendErr.Message != "backend produced no usable canonical output" {
		t.Fatalf("image-only error = %T %v, want backend output-contract failure", err, err)
	}
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
