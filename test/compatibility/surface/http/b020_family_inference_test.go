package http_test

import (
	"testing"

	"github.com/metrofun/swobu/internal/domain/compatibility"
)

func TestB020_FamilyInference(t *testing.T) {
	tests := []struct {
		name                string
		method              string
		rawPath             string
		hasAnthropicVersion bool
		want                compatibility.IngressFamily
	}{
		{name: "POST chat completions", method: "POST", rawPath: "/chat/completions", want: compatibility.IngressFamilyChatCompletions},
		{name: "POST responses", method: "POST", rawPath: "/responses", want: compatibility.IngressFamilyResponses},
		{name: "POST completions", method: "POST", rawPath: "/completions", want: compatibility.IngressFamilyCompletions},
		{name: "POST v1 messages with anthropic version", method: "POST", rawPath: "/v1/messages", hasAnthropicVersion: true, want: compatibility.IngressFamilyMessages},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized, err := compatibility.NormalizePath(tt.rawPath)
			if err != nil {
				t.Fatalf("NormalizePath returned error: %v", err)
			}

			got, err := compatibility.InferFamily(tt.method, normalized, tt.hasAnthropicVersion)
			if err != nil {
				t.Fatalf("InferFamily returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("InferFamily() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestB020_FamilyInferenceRejectsAmbiguousOrUnsupportedIngress(t *testing.T) {
	tests := []struct {
		name                string
		method              string
		rawPath             string
		hasAnthropicVersion bool
	}{
		{name: "POST messages without anthropic version", method: "POST", rawPath: "/messages"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalized, err := compatibility.NormalizePath(tt.rawPath)
			if err != nil {
				t.Fatalf("NormalizePath returned error: %v", err)
			}

			_, err = compatibility.InferFamily(tt.method, normalized, tt.hasAnthropicVersion)
			if err == nil {
				t.Fatal("expected unsupported endpoint error, got nil")
			}
		})
	}
}
