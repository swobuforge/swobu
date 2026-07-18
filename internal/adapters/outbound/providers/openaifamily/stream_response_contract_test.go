package openaifamily

import "testing"

func TestIsSSEContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "exact", raw: "text/event-stream", want: true},
		{name: "parameters", raw: "text/event-stream; charset=utf-8", want: true},
		{name: "case insensitive", raw: "Text/Event-Stream", want: true},
		{name: "missing", raw: "", want: false},
		{name: "json document", raw: "application/json", want: false},
		{name: "lookalike", raw: "text/event-streamish", want: false},
		{name: "malformed", raw: `text/event-stream; charset="`, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isSSEContentType(tt.raw); got != tt.want {
				t.Fatalf("isSSEContentType(%q)=%t want=%t", tt.raw, got, tt.want)
			}
		})
	}
}
