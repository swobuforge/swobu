package replay

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestNativeRefInvalidWhenModelDiffers(t *testing.T) {
	a := TargetKey{ProviderSpec: "openai", Protocol: protocolkind.Responses, ProviderProtocol: "responses", BaseURL: "https://api.openai.com", AuthScope: "default", ModelID: "gpt-4o"}
	b := TargetKey{ProviderSpec: "openai", Protocol: protocolkind.Responses, ProviderProtocol: "responses", BaseURL: "https://api.openai.com", AuthScope: "default", ModelID: "gpt-4o-mini"}
	if a.Equal(b) {
		t.Error("expected different ModelID to cause inequality")
	}
}

func TestNativeRefInvalidWhenBaseURLDiffers(t *testing.T) {
	a := TargetKey{ProviderSpec: "openai", Protocol: protocolkind.Responses, ProviderProtocol: "responses", BaseURL: "https://api.openai.com", AuthScope: "default", ModelID: "gpt-4o"}
	b := TargetKey{ProviderSpec: "openai", Protocol: protocolkind.Responses, ProviderProtocol: "responses", BaseURL: "https://api.openai.com/v2", AuthScope: "default", ModelID: "gpt-4o"}
	if a.Equal(b) {
		t.Error("expected different BaseURL to cause inequality")
	}
}

func TestNativeRefInvalidWhenCredentialDiffers(t *testing.T) {
	a := TargetKey{ProviderSpec: "openai", Protocol: protocolkind.Responses, ProviderProtocol: "responses", BaseURL: "https://api.openai.com", AuthScope: "default", ModelID: "gpt-4o"}
	b := TargetKey{ProviderSpec: "openai", Protocol: protocolkind.Responses, ProviderProtocol: "responses", BaseURL: "https://api.openai.com", AuthScope: "other", ModelID: "gpt-4o"}
	if a.Equal(b) {
		t.Error("expected different AuthScope to cause inequality")
	}
}

func TestNativeRefInvalidWhenProtocolDiffers(t *testing.T) {
	a := TargetKey{ProviderSpec: "openai", Protocol: protocolkind.Responses, ProviderProtocol: "responses", BaseURL: "https://api.openai.com", AuthScope: "default", ModelID: "gpt-4o"}
	b := TargetKey{ProviderSpec: "openai", Protocol: protocolkind.Messages, ProviderProtocol: "messages", BaseURL: "https://api.openai.com", AuthScope: "default", ModelID: "gpt-4o"}
	if a.Equal(b) {
		t.Error("expected different Protocol to cause inequality")
	}
}

func TestTargetKeyEqualExactMatch(t *testing.T) {
	a := TargetKey{ProviderSpec: "openai", Protocol: protocolkind.Responses, ProviderProtocol: "responses", BaseURL: "https://api.openai.com", AuthScope: "default", ModelID: "gpt-4o"}
	if !a.Equal(a) {
		t.Error("expected exact same target to be equal")
	}
}
