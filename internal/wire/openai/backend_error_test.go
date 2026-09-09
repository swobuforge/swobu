package openai

import (
	"net/http"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestParseBackendErrorPreservesOpenAIFields(t *testing.T) {
	opaque := canonical.NewBackendError("target-a", 429, `{"error":{"type":"rate_limit_error","code":"quota","message":"slow down","param":"model"}}`, "30")
	err := ParseBackendError(opaque, protocolkind.Responses, "req_provider")
	if err.ProviderError == nil || err.SourceProtocol != protocolkind.Responses || err.ProviderError.Param != "model" || err.ProviderError.RequestID != "req_provider" {
		t.Fatalf("structured error = %#v", err)
	}
}

func TestParseBackendErrorLeavesMalformedBodyOpaque(t *testing.T) {
	opaque := canonical.NewBackendError("target-a", http.StatusBadGateway, `{"error":`, "")
	err := ParseBackendError(opaque, protocolkind.Responses, "")
	if err.ProviderError != nil || err.SourceProtocol != "" || err.Message != opaque.Message {
		t.Fatalf("opaque error promoted = %#v", err)
	}
}
