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
	image, _ := NewInlineImage(ImageMediaPNG, []byte("png"), Specified[ImageDetail]{})
	message, _ := NewMessageItem(MessageRoleUser, []MessagePart{NewTextMessagePart("see"), NewImageMessagePart(image)})
	request := NewCanonicalRequest(RequestParams{Items: []CanonicalItem{message}})
	var seen RequestPartRef
	if err := WalkRequestImages(request, func(position RequestPartRef, placement ImagePlacement, _ ImagePart) error {
		seen = position
		if placement != ImageInMessage {
			t.Fatalf("placement = %q", placement)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if seen != (RequestPartRef{Item: 0, Part: 1}) {
		t.Fatalf("position = %#v", seen)
	}
}
