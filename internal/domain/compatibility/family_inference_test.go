package compatibility

import (
	"errors"
	"testing"
)

func TestInferFamily_UsesExplicitRequestAndPathRules(t *testing.T) {
	tests := []struct {
		name                string
		method              string
		path                NormalizedPath
		hasAnthropicVersion bool
		want                IngressFamily
	}{
		{name: "chat completions POST", method: "POST", path: NormalizedPathChatCompletions, want: IngressFamilyChatCompletions},
		{name: "responses POST", method: "POST", path: NormalizedPathResponses, want: IngressFamilyResponses},
		{name: "completions POST", method: "POST", path: NormalizedPathCompletions, want: IngressFamilyCompletions},
		{name: "messages POST with anthropic version", method: "POST", path: NormalizedPathMessages, hasAnthropicVersion: true, want: IngressFamilyMessages},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := InferFamily(tt.method, tt.path, tt.hasAnthropicVersion)
			if err != nil {
				t.Fatalf("InferFamily returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("InferFamily() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateIngressTransport(t *testing.T) {
	t.Run("models GET is accepted", func(t *testing.T) {
		if err := ValidateIngressTransport("GET", NormalizedPathModels, false); err != nil {
			t.Fatalf("ValidateIngressTransport returned error: %v", err)
		}
	})
	t.Run("websocket upgrade is explicitly rejected", func(t *testing.T) {
		err := ValidateIngressTransport("GET", NormalizedPathResponses, true)
		if err != nil {
			t.Fatalf("ValidateIngressTransport returned error: %v", err)
		}
	})
	t.Run("websocket upgrade on non-responses routes is explicitly rejected", func(t *testing.T) {
		err := ValidateIngressTransport("GET", NormalizedPathChatCompletions, true)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var compatErr Error
		if !errors.As(err, &compatErr) {
			t.Fatalf("error type = %T, want compatibility.Error", err)
		}
		if compatErr.Code != ErrorCodeUnsupportedEndpoint {
			t.Fatalf("error code = %q, want %q", compatErr.Code, ErrorCodeUnsupportedEndpoint)
		}
	})
	t.Run("non-post family operation is explicitly rejected", func(t *testing.T) {
		err := ValidateIngressTransport("GET", NormalizedPathResponses, false)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestInferFamily_RejectsUnsupportedOrAmbiguousIngress(t *testing.T) {
	tests := []struct {
		name                string
		method              string
		path                NormalizedPath
		hasAnthropicVersion bool
	}{
		{name: "messages without anthropic version is ambiguous", method: "POST", path: NormalizedPathMessages},
		{name: "messages GET is unsupported", method: "GET", path: NormalizedPathMessages, hasAnthropicVersion: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := InferFamily(tt.method, tt.path, tt.hasAnthropicVersion)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			var compatErr Error
			if !errors.As(err, &compatErr) {
				t.Fatalf("expected compatibility.Error, got %T", err)
			}
			if compatErr.Code != ErrorCodeUnsupportedEndpoint {
				t.Fatalf("error code = %q, want %q", compatErr.Code, ErrorCodeUnsupportedEndpoint)
			}
		})
	}
}
