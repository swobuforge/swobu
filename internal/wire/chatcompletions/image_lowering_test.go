package chatcompletions

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestChatAssistantImageFailsAsClientOutputContract(t *testing.T) {
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

func TestChatStreamedAssistantImageFailsAsClientOutputContract(t *testing.T) {
	response := chatAssistantImageResponse(t)
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

func chatAssistantImageResponse(t *testing.T) canonical.CanonicalResponse {
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

func TestEncodeChatImageOriginalMapsHighWithDecision(t *testing.T) {
	image, _ := canonical.NewURLImage("https://example.test/original.png", canonical.Specify(canonical.ImageDetailOriginal))
	message, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewImageMessagePart(image)})
	req := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{message}})

	sink := &recordingDecisionSink{}
	doc, err := EncodeCarrierWithDecisions(req, delivery.BufferedDelivery(), sink, "ex")
	if err != nil {
		t.Fatal(err)
	}
	if len(sink.effects) != 1 || sink.effects[0].Feature != compat.RequestItemsMessageImageDetail || sink.effects[0].Outcome != compat.Approx {
		t.Fatalf("decisions = %#v", sink.effects)
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
	sink := &recordingDecisionSink{}
	doc, err := EncodeCarrierWithDecisions(req, delivery.BufferedDelivery(), sink, "ex")
	if err == nil {
		t.Fatal("Chat Completions accepted a tool-result image")
	}
	if !doc.IsEmpty() || !decisionRecorded(sink.effects, compat.RequestItemsToolResultImage, compat.Reject) ||
		!decisionRecorded(sink.effects, compat.RequestItemsToolResultContentBoundaries, compat.Approx) {
		t.Fatalf("rejection doc=%#v decisions=%#v", doc, sink.effects)
	}
}

func TestEncodeChatUserImages_PreservesURLAndInlineSources(t *testing.T) {
	urlImage, _ := canonical.NewURLImage("https://example.test/image.png", canonical.Specify(canonical.ImageDetailHigh))
	inlineImage, _ := canonical.NewInlineImage(canonical.ImageMediaPNG, imageTestPNG(), canonical.Specify(canonical.ImageDetailLow))
	message, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{
		canonical.NewImageMessagePart(urlImage), canonical.NewImageMessagePart(inlineImage),
	})
	req := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{message}})
	doc, err := EncodeCarrierWithDecisions(req, delivery.BufferedDelivery(), nil, "")
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
