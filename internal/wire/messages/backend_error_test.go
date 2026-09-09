package messages

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestParseBackendErrorPreservesMessagesFields(t *testing.T) {
	opaque := canonical.NewBackendError("target-a", 529, `{"type":"error","error":{"type":"overloaded_error","message":"try later"},"request_id":"req_provider"}`, "10")
	err := ParseBackendError(opaque)
	if err.ProviderError == nil || err.SourceProtocol != protocolkind.Messages || err.ProviderError.Type != "overloaded_error" || err.ProviderError.RequestID != "req_provider" {
		t.Fatalf("structured error = %#v", err)
	}
}
