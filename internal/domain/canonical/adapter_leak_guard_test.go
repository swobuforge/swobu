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

func TestCanonicalItem_ToolInputIsRawPayloadNotMap(t *testing.T) {
	t.Parallel()

	field, ok := reflect.TypeOf(CanonicalItem{}).FieldByName("Input")
	if !ok {
		t.Fatal("CanonicalItem must expose Input field")
	}
	// Tool input stays a raw payload wrapper so protocol decoders never infer a
	// fake semantic map shape at the canonical boundary.
	if field.Type.Kind() == reflect.Map {
		t.Fatal("CanonicalItem.Input must not be map-shaped")
	}
	if field.Type.Name() != "ToolArguments" {
		t.Fatalf("CanonicalItem.Input type = %q, want ToolArguments", field.Type.Name())
	}
}
