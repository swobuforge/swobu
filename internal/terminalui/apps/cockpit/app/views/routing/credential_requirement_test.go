package routing

import "testing"

func TestProviderCredentialSelectionRequired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		provider      string
		baseURL       string
		credentialRef string
		want          bool
	}{
		{
			name:     "openrouter requires credential",
			provider: "openrouter",
			baseURL:  "https://openrouter.ai/api/v1",
			want:     true,
		},
		{
			name:     "ollama does not require credential",
			provider: "ollama",
			baseURL:  "http://127.0.0.1:11434/v1",
			want:     false,
		},
		{
			name:     "OpenAI-compatible remote requires credential",
			provider: "openai_compatible",
			baseURL:  "https://api.example.com/v1",
			want:     true,
		},
		{
			name:     "OpenAI-compatible local does not require credential",
			provider: "openai_compatible",
			baseURL:  "http://localhost:11434/v1",
			want:     false,
		},
		{
			name:          "existing credential keeps row visible for non-required provider",
			provider:      "ollama",
			baseURL:       "http://127.0.0.1:11434/v1",
			credentialRef: "env:OLLAMA_API_KEY",
			want:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := providerCredentialSelectionRequired(tt.provider, tt.baseURL, tt.credentialRef)
			if got != tt.want {
				t.Fatalf("providerCredentialSelectionRequired(%q,%q,%q)=%v want %v", tt.provider, tt.baseURL, tt.credentialRef, got, tt.want)
			}
		})
	}
}
