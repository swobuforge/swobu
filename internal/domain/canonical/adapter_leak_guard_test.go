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

	paramsType := reflect.TypeOf(RequestParams{})
	if _, ok := paramsType.FieldByName("ConversationID"); ok {
		t.Fatal("RequestParams must not accept conversation selector from client boundary")
	}
	if _, ok := paramsType.FieldByName("PreviousResponseID"); ok {
		t.Fatal("RequestParams must not accept raw previous_response_id selector")
	}
}
