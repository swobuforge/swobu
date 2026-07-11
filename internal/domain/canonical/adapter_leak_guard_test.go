package canonical

import (
	"reflect"
	"testing"
)

func TestCanonicalRequest_DoesNotExposeClientAdapterSelectors(t *testing.T) {
	t.Parallel()

	reqType := reflect.TypeOf(CanonicalRequest{})
	if _, ok := reqType.MethodByName("ConversationID"); ok {
		t.Fatal("CanonicalRequest must not expose conversation selector from client ingress")
	}

	paramsType := reflect.TypeOf(RequestParams{})
	if _, ok := paramsType.FieldByName("ConversationID"); ok {
		t.Fatal("RequestParams must not accept conversation selector from client ingress")
	}
}
