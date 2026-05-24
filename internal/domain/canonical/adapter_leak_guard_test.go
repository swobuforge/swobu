package canonical

import (
	"reflect"
	"testing"
)

func TestGenerationCanonicalRequest_DoesNotExposeClientAdapterSelectors(t *testing.T) {
	t.Parallel()

	reqType := reflect.TypeOf(GenerationCanonicalRequest{})
	if _, ok := reqType.MethodByName("ConversationID"); ok {
		t.Fatal("GenerationCanonicalRequest must not expose conversation selector from client ingress")
	}

	paramsType := reflect.TypeOf(GenerationRequestParams{})
	if _, ok := paramsType.FieldByName("ConversationID"); ok {
		t.Fatal("GenerationRequestParams must not accept conversation selector from client ingress")
	}
}
