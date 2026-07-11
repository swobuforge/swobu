package canonical

import (
	"errors"
	"testing"
)

func TestInferClientFamily_UsesExplicitRequestAndPathRules(t *testing.T) {
	tests := []struct {
		name                      string
		method                    string
		path                      NormalizedPath
		hasMessagesProtocolMarker bool
		want                      ClientFamily
	}{
		{name: "chat completions POST", method: "POST", path: NormalizedPathChatCompletions, want: ClientFamilyChatCompletions},
		{name: "responses POST", method: "POST", path: NormalizedPathResponses, want: ClientFamilyResponses},
		{name: "completions POST", method: "POST", path: NormalizedPathCompletions, want: ClientFamilyCompletions},
		{name: "messages POST with protocol marker", method: "POST", path: NormalizedPathMessages, hasMessagesProtocolMarker: true, want: ClientFamilyMessages},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := InferClientFamily(tt.method, tt.path, tt.hasMessagesProtocolMarker)
			if err != nil {
				t.Fatalf("InferClientFamily returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("InferClientFamily() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateClientTransport(t *testing.T) {
	t.Run("models GET is accepted", func(t *testing.T) {
		if err := ValidateClientTransport("GET", NormalizedPathModels, false); err != nil {
			t.Fatalf("ValidateClientTransport returned error: %v", err)
		}
	})
	t.Run("websocket upgrade is explicitly rejected", func(t *testing.T) {
		err := ValidateClientTransport("GET", NormalizedPathResponses, true)
		if err != nil {
			t.Fatalf("ValidateClientTransport returned error: %v", err)
		}
	})
	t.Run("websocket upgrade on non-responses routes is explicitly rejected", func(t *testing.T) {
		err := ValidateClientTransport("GET", NormalizedPathChatCompletions, true)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		var compatErr Error
		if !errors.As(err, &compatErr) {
			t.Fatalf("error type = %T, want canonical.Error", err)
		}
		if compatErr.Code != ErrorCodeUnsupportedEndpoint {
			t.Fatalf("error code = %q, want %q", compatErr.Code, ErrorCodeUnsupportedEndpoint)
		}
	})
	t.Run("non-post family operation is explicitly rejected", func(t *testing.T) {
		err := ValidateClientTransport("GET", NormalizedPathResponses, false)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestInferClientFamily_RejectsUnsupportedOrAmbiguousClient(t *testing.T) {
	tests := []struct {
		name                      string
		method                    string
		path                      NormalizedPath
		hasMessagesProtocolMarker bool
	}{
		{name: "messages without protocol marker is ambiguous", method: "POST", path: NormalizedPathMessages},
		{name: "messages GET is unsupported", method: "GET", path: NormalizedPathMessages, hasMessagesProtocolMarker: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := InferClientFamily(tt.method, tt.path, tt.hasMessagesProtocolMarker)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			var compatErr Error
			if !errors.As(err, &compatErr) {
				t.Fatalf("expected canonical.Error, got %T", err)
			}
			if compatErr.Code != ErrorCodeUnsupportedEndpoint {
				t.Fatalf("error code = %q, want %q", compatErr.Code, ErrorCodeUnsupportedEndpoint)
			}
		})
	}
}
