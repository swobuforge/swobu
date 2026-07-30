package chatcompletions

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeChatProviderAssistantImageBesideTextDropsImage(t *testing.T) {
	raw := []byte(`{"id":"chat_1","model":"m","choices":[{"message":{"role":"assistant","content":[{"type":"text","text":"Here is the result"},{"type":"image_url","image_url":{"url":"https://example.test/output.png"}}]},"finish_reason":"stop"}]}`)
	var changes []compat.Change
	stream, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, raw, "ex_image", &changes)
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
		position, ok := decision.Occurrence.ResponsePart()
		if decision.Capability == canonical.ResponseItemsKind && decision.Kind == compat.Omission &&
			ok && position.Item == 0 && position.Part == 1 {
			drops++
		}
	}
	if drops != 1 {
		t.Fatalf("assistant image omission changes = %d, want 1; all=%#v", drops, changes)
	}
}

func TestDecodeChatProviderImageOnlyFailsOutputContract(t *testing.T) {
	raw := []byte(`{"id":"chat_1","model":"m","choices":[{"message":{"role":"assistant","content":[{"type":"image_url","image_url":{"url":"https://example.test/output.png"}}]},"finish_reason":"stop"}]}`)
	_, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, raw, "ex_image", nil)
	var backendErr canonical.BackendError
	if !errors.As(err, &backendErr) || backendErr.Message != "backend produced no usable canonical output" {
		t.Fatalf("image-only error = %T %v, want backend output-contract failure", err, err)
	}
}

func TestEncodeChatImageOriginalMapsHighWithDecision(t *testing.T) {
	image, _ := canonical.NewURLImage("https://example.test/original.png", canonical.Specify(canonical.ImageDetailOriginal))
	message, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewImageMessagePart(image)})
	req := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{message}})

	var changes []compat.Change
	doc, err := EncodeCarrierWithChanges(req, delivery.BufferedDelivery(), &changes, "ex")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Capability != canonical.RequestItemsMessageImageDetail || changes[0].Kind != compat.Approximation {
		t.Fatalf("changes = %#v", changes)
	}
	if !jsonBodyContains(t, doc.Raw, `"detail":"high"`) {
		t.Fatalf("image did not map original to high: %s", doc.Raw)
	}
}

func TestEncodeChatToolResultImageRejectsWithoutClosedCallBatch(t *testing.T) {
	image, _ := canonical.NewInlineImage(canonical.ImageMediaPNG, imageTestPNG(), canonical.Unspecified[canonical.ImageDetail]())
	callID, _ := canonical.NewToolCallID("call_image")
	result, _ := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("must not escape"), canonical.NewImageToolResultPart(image)}, false)
	req := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{result}})
	var changes []compat.Change
	doc, err := EncodeCarrierWithChanges(req, delivery.BufferedDelivery(), &changes, "ex")
	if err == nil {
		t.Fatal("Chat Completions accepted a tool-result image")
	}
	if !doc.IsEmpty() {
		t.Fatalf("rejection doc=%#v changes=%#v", doc, changes)
	}
}

func TestEncodeChatUserImages_PreservesURLAndInlineSources(t *testing.T) {
	urlImage, _ := canonical.NewURLImage("https://example.test/image.png", canonical.Specify(canonical.ImageDetailHigh))
	inlineImage, _ := canonical.NewInlineImage(canonical.ImageMediaPNG, imageTestPNG(), canonical.Specify(canonical.ImageDetailLow))
	message, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{
		canonical.NewImageMessagePart(urlImage), canonical.NewImageMessagePart(inlineImage),
	})
	req := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{message}})
	doc, err := EncodeCarrierWithChanges(req, delivery.BufferedDelivery(), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	body := string(doc.Raw)
	if !strings.Contains(body, `"url":"https://example.test/image.png"`) ||
		!strings.Contains(body, `"url":"data:image/png;base64,`) ||
		!strings.Contains(body, `"detail":"high"`) || !strings.Contains(body, `"detail":"low"`) {
		t.Fatalf("Chat Completions image content = %s", body)
	}
}

func imageTestPNG() []byte {
	return []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
}

func jsonBodyContains(t *testing.T, raw []byte, want string) bool {
	t.Helper()
	return string(raw) != "" && containsJSONFragment(string(raw), want)
}

func containsJSONFragment(raw, fragment string) bool {
	return len(fragment) == 0 || len(raw) >= len(fragment) && func() bool {
		for i := 0; i+len(fragment) <= len(raw); i++ {
			if raw[i:i+len(fragment)] == fragment {
				return true
			}
		}
		return false
	}()
}
