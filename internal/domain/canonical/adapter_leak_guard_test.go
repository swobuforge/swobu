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

func TestCanonicalItem_HasPrivateClosedBranchesAndTypedToolInput(t *testing.T) {
	t.Parallel()

	itemType := reflect.TypeOf(CanonicalItem{})
	for i := 0; i < itemType.NumField(); i++ {
		if itemType.Field(i).IsExported() {
			t.Fatalf("CanonicalItem field %q must remain private", itemType.Field(i).Name)
		}
	}
	inputType := reflect.TypeOf(ToolInput{})
	for i := 0; i < inputType.NumField(); i++ {
		if inputType.Field(i).IsExported() {
			t.Fatalf("ToolInput field %q must remain private", inputType.Field(i).Name)
		}
	}
}

func TestCanonicalResponseConstructorIsCheckedAndTakesResponseRef(t *testing.T) {
	t.Parallel()
	constructor := reflect.TypeOf(NewCanonicalResponse)
	if got, want := constructor.In(0), reflect.TypeOf(ResponseRef{}); got != want {
		t.Fatalf("response constructor identity = %v, want %v", got, want)
	}
	if constructor.NumOut() != 2 || constructor.Out(1) != reflect.TypeOf((*error)(nil)).Elem() {
		t.Fatal("response constructor is not checked")
	}
}
