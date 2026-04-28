package http_test

import (
	"testing"

	"github.com/metrofun/swobu/internal/domain/compatibility"
)

func TestB010_PathNormalization(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want compatibility.NormalizedPath
	}{
		{name: "v1 chat completions", raw: "/v1/chat/completions", want: compatibility.NormalizedPathChatCompletions},
		{name: "chat completions", raw: "/chat/completions", want: compatibility.NormalizedPathChatCompletions},
		{name: "v1 responses", raw: "/v1/responses", want: compatibility.NormalizedPathResponses},
		{name: "responses", raw: "/responses", want: compatibility.NormalizedPathResponses},
		{name: "v1 completions", raw: "/v1/completions", want: compatibility.NormalizedPathCompletions},
		{name: "completions", raw: "/completions", want: compatibility.NormalizedPathCompletions},
		{name: "v1 models", raw: "/v1/models", want: compatibility.NormalizedPathModels},
		{name: "models", raw: "/models", want: compatibility.NormalizedPathModels},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := compatibility.NormalizePath(tt.raw)
			if err != nil {
				t.Fatalf("NormalizePath returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("NormalizePath(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}

	for _, raw := range []string{"/v1/embeddings"} {
		if _, err := compatibility.NormalizePath(raw); err == nil {
			t.Fatalf("expected unsupported normalized path error for %q, got nil", raw)
		}
	}
}
