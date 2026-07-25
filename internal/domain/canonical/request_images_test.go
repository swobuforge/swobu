package canonical

import "testing"

func TestToolCallInputMustMatchToolKind(t *testing.T) {
	callID, _ := NewToolCallID("call_1")
	function, _ := NewRequestToolKey(ToolKindFunction, "lookup")
	custom, _ := NewRequestToolKey(ToolKindCustom, "shell")
	object, _ := ParseJSONObject([]byte(`{"city":"London"}`))
	if _, err := NewToolCallItem(callID, function, NewTextToolInput("London")); err == nil {
		t.Fatal("function call accepted text input")
	}
	if _, err := NewToolCallItem(callID, custom, NewJSONObjectToolInput(object)); err == nil {
		t.Fatal("custom call accepted object input")
	}
}

func TestURLImageRejectsFragment(t *testing.T) {
	if _, err := NewURLImage("https://example.test/image.png#frame", Specified[ImageDetail]{}); err == nil {
		t.Fatal("URL image accepted a fragment that is never sent to the origin")
	}
}

func TestWalkAndRewriteRequestImagesPreservePlacement(t *testing.T) {
	urlImage, _ := NewURLImage("https://example.test/image.png", Specify(ImageDetailHigh))
	message, _ := NewMessageItem(MessageRoleUser, []MessagePart{NewTextMessagePart("see"), NewImageMessagePart(urlImage)})
	callID, _ := NewToolCallID("call_image")
	result, _ := NewToolResultItem(callID, []ToolResultPart{
		NewTextToolResultPart("before"),
		NewImageToolResultPart(urlImage),
		NewTextToolResultPart("after"),
	}, true)
	request := NewCanonicalRequest(RequestParams{Items: []CanonicalItem{message, result}})
	var seen []struct {
		position  RequestPartRef
		placement ImagePlacement
	}
	if err := WalkRequestImages(request, func(position RequestPartRef, placement ImagePlacement, _ ImagePart) error {
		seen = append(seen, struct {
			position  RequestPartRef
			placement ImagePlacement
		}{position: position, placement: placement})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 ||
		seen[0].position != (RequestPartRef{Item: 0, Part: 1}) || seen[0].placement != ImageInMessage ||
		seen[1].position != (RequestPartRef{Item: 1, Part: 1}) || seen[1].placement != ImageInToolResult {
		t.Fatalf("image coordinates = %#v", seen)
	}

	replacement, _ := NewInlineImage(ImageMediaPNG, []byte("replacement"), Specify(ImageDetailHigh))
	rewritten, err := RewriteRequestImages(request, func(_ RequestPartRef, _ ImagePlacement, _ ImagePart) (ImagePart, error) {
		return replacement, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	items := rewritten.Items()
	if len(items) != 2 || items[0].Kind() != ItemKindMessage || items[1].Kind() != ItemKindToolResult {
		t.Fatalf("rewritten topology = %#v", items)
	}
	rewrittenMessage, _ := items[0].Message()
	rewrittenResult, _ := items[1].ToolResult()
	if len(rewrittenMessage.Content()) != 2 || len(rewrittenResult.Content()) != 3 ||
		rewrittenResult.CallID() != callID || !rewrittenResult.IsError() {
		t.Fatalf("rewritten boundaries = message %#v result %#v", rewrittenMessage, rewrittenResult)
	}
	if rewrittenMessage.Content()[1].Kind() != PartKindImage ||
		rewrittenResult.Content()[0].Kind() != PartKindText ||
		rewrittenResult.Content()[1].Kind() != PartKindImage ||
		rewrittenResult.Content()[2].Kind() != PartKindText {
		t.Fatalf("rewritten part order changed")
	}
}
