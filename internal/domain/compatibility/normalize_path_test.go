package compatibility

import (
	"errors"
	"testing"
)

func TestNormalizePath_MapsSupportedAliasesToCanonicalPaths(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want NormalizedPath
	}{
		{name: "chat without version", raw: "/chat/completions", want: NormalizedPathChatCompletions},
		{name: "chat with version", raw: "/v1/chat/completions", want: NormalizedPathChatCompletions},
		{name: "responses without version", raw: "/responses", want: NormalizedPathResponses},
		{name: "responses with version", raw: "/v1/responses", want: NormalizedPathResponses},
		{name: "completions without version", raw: "/completions", want: NormalizedPathCompletions},
		{name: "completions with version", raw: "/v1/completions", want: NormalizedPathCompletions},
		{name: "messages without version", raw: "/messages", want: NormalizedPathMessages},
		{name: "messages with version", raw: "/v1/messages", want: NormalizedPathMessages},
		{name: "models without version", raw: "/models", want: NormalizedPathModels},
		{name: "models with version", raw: "/v1/models", want: NormalizedPathModels},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizePath(tt.raw)
			if err != nil {
				t.Fatalf("NormalizePath returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("NormalizePath(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestNormalizePath_RejectsUnsupportedPaths(t *testing.T) {
	for _, raw := range []string{"/v1/embeddings"} {
		_, err := NormalizePath(raw)
		if err == nil {
			t.Fatalf("expected error for %q, got nil", raw)
		}
		var compatErr Error
		if !errors.As(err, &compatErr) {
			t.Fatalf("expected compatibility.Error for %q, got %T", raw, err)
		}
		if compatErr.Code != ErrorCodeUnsupportedEndpoint {
			t.Fatalf("error code for %q = %q, want %q", raw, compatErr.Code, ErrorCodeUnsupportedEndpoint)
		}
	}
}
