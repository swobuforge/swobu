package canonical

import (
	"reflect"
	"testing"
)

func TestCanonicalRequest_DoesNotExposeClientAdapterSelectors(t *testing.T) {
	t.Parallel()

	reqType := reflect.TypeOf(CanonicalRequest{})
	if _, ok := reqType.MethodByName("ConversationID"); ok {
		t.Fatal("CanonicalRequest must not expose conversation selector from client boundary")
	}
	if _, ok := reqType.MethodByName("PreviousResponseID"); ok {
		t.Fatal("CanonicalRequest must not expose raw previous_response_id selector")
	}
	// ToolMode is intentionally absent from the canonical request surface.
	if _, ok := reqType.MethodByName("ToolMode"); ok {
		t.Fatal("CanonicalRequest must not expose ToolMode compatibility shim")
	}

	paramsType := reflect.TypeOf(RequestParams{})
	if _, ok := paramsType.FieldByName("ConversationID"); ok {
		t.Fatal("RequestParams must not accept conversation selector from client boundary")
	}
	if _, ok := paramsType.FieldByName("PreviousResponseID"); ok {
		t.Fatal("RequestParams must not accept raw previous_response_id selector")
	}
	if _, ok := paramsType.FieldByName("ToolMode"); ok {
		t.Fatal("RequestParams must not accept ToolMode compatibility shim")
	}
}

func TestCanonicalItem_HasOnePrivatePayloadAndRawToolInput(t *testing.T) {
	t.Parallel()

	itemType := reflect.TypeOf(CanonicalItem{})
	for i := 0; i < itemType.NumField(); i++ {
		if itemType.Field(i).IsExported() {
			t.Fatalf("CanonicalItem field %q must remain private", itemType.Field(i).Name)
		}
	}
	field, ok := reflect.TypeOf(ToolUseItemPayload{}).FieldByName("Input")
	if !ok {
		t.Fatal("ToolUseItemPayload must own Input")
	}
	if field.Type.Kind() == reflect.Map {
		t.Fatal("ToolUseItemPayload.Input must not be map-shaped")
	}
	if field.Type.Name() != "ToolArguments" {
		t.Fatalf("ToolUseItemPayload.Input type = %q, want ToolArguments", field.Type.Name())
	}
}

func TestOutputConstructorsRequireSwobuResponseID(t *testing.T) {
	t.Parallel()
	want := reflect.TypeOf(SwobuResponseID(""))
	for name, constructor := range map[string]any{
		"conversation":            NewConversationOutput,
		"conversation with usage": NewConversationOutputWithUsage,
		"prompt":                  NewPromptOutput,
		"prompt with usage":       NewPromptOutputWithUsage,
	} {
		if got := reflect.TypeOf(constructor).In(0); got != want {
			t.Fatalf("%s response ID parameter = %v, want %v", name, got, want)
		}
	}
}
