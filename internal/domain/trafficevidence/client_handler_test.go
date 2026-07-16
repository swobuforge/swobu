package trafficevidence

import "testing"

func TestNormalizeClientHandler(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want ClientHandler
	}{
		{name: "codex user agent", raw: "Codex/1.2", want: ClientHandler("Codex/1.2")},
		{name: "claude code user agent", raw: "Claude-Code/2.0", want: ClientHandler("Claude-Code/2.0")},
		{name: "leading and trailing space", raw: "  Aider/0.82  ", want: ClientHandler("Aider/0.82")},
		{name: "multi-token user agent", raw: "opencode/1.15.13 ai-sdk/provider-utils/4.0.23 runtime/bun/1.3.14 pr", want: ClientHandler("opencode/1.15.13")},
		{name: "tab delimiter", raw: "opencode/1.0\tsdk/2.0", want: ClientHandler("opencode/1.0")},
		{name: "blank", raw: " ", want: ClientHandlerUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeClientHandler(tt.raw); got != tt.want {
				t.Fatalf("NormalizeClientHandler(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}
