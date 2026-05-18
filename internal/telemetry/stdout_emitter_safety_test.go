package telemetry

import (
	"context"
	"strings"
	"testing"
)

func TestStdoutEmitter_ErrorTracePayload_DoesNotContainForbiddenTelemetryTokens(t *testing.T) {
	t.Parallel()

	var sink strings.Builder
	emitter := NewStdoutEmitter(&sink)
	durationMS := 1450
	emitter.EmitErrorTrace(context.Background(), ErrorTracePayload{
		StatusCode:    500,
		ResultClass:   "backend_error",
		ProviderRoute: "openai:gpt-4.1",
		Operation:     "responses.create",
		DurationMS:    &durationMS,
	})

	payload := strings.ToLower(sink.String())
	for _, forbidden := range loadForbiddenTokensFixture(t) {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("payload leaked forbidden token %q: %s", forbidden, sink.String())
		}
	}
}
