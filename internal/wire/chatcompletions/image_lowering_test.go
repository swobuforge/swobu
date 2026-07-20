package chatcompletions

import (
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestEncodeChatImageOriginal_StrictRejectsCompatMapsHighWithDecision(t *testing.T) {
	image, _ := canonical.NewURLImage("https://example.test/original.png", canonical.Specify(canonical.ImageDetailOriginal))
	message, _ := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewImageMessagePart(image)})
	req := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{message}})

	strict := EncodeOptions{MaxOutputTokensField: MaxOutputTokensFieldCompletion, Compatibility: compat.CompatibilityPolicy{Mode: compat.CompatibilityStrict}}
	if _, err := EncodeCarrierWithDecisions(req, delivery.BufferedDelivery(), nil, "", strict); err == nil {
		t.Fatal("strict Chat Completions lowering accepted original image detail")
	}
	sink := &recordingDecisionSink{}
	compatOptions := EncodeOptions{MaxOutputTokensField: MaxOutputTokensFieldCompletion}
	doc, err := EncodeCarrierWithDecisions(req, delivery.BufferedDelivery(), sink, "ex", compatOptions)
	if err != nil {
		t.Fatal(err)
	}
	if len(sink.effects) != 1 || sink.effects[0].Feature != compat.RequestItemsMessageImageDetail || sink.effects[0].Outcome != compat.Approx {
		t.Fatalf("decisions = %#v", sink.effects)
	}
	if !jsonBodyContains(t, doc.Raw, `"detail":"high"`) {
		t.Fatalf("compat image did not map original to high: %s", doc.Raw)
	}
}

func TestEncodeChatToolResultImageRejects(t *testing.T) {
	image, _ := canonical.NewInlineImage(canonical.ImageMediaPNG, imageTestPNG(), canonical.Unspecified[canonical.ImageDetail]())
	callID, _ := canonical.NewToolCallID("call_image")
	result, _ := canonical.NewToolResultItem(callID, []canonical.ToolResultPart{canonical.NewTextToolResultPart("must not escape"), canonical.NewImageToolResultPart(image)}, false)
	req := canonical.NewCanonicalRequest(canonical.RequestParams{Model: canonical.Specify("m"), Items: []canonical.CanonicalItem{result}})
	sink := &recordingDecisionSink{}
	doc, err := EncodeCarrierWithDecisions(req, delivery.BufferedDelivery(), sink, "ex", EncodeOptions{MaxOutputTokensField: MaxOutputTokensFieldCompletion})
	if err == nil {
		t.Fatal("Chat Completions accepted a tool-result image")
	}
	if !doc.IsEmpty() || len(sink.effects) != 1 || sink.effects[0].Feature != compat.RequestItemsToolResultImage || sink.effects[0].Outcome != compat.Reject {
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
	doc, err := EncodeCarrierWithDecisions(req, delivery.BufferedDelivery(), nil, "", EncodeOptions{MaxOutputTokensField: MaxOutputTokensFieldCompletion})
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
