package telemetry

import (
	"context"
	"strings"
	"testing"
)

func TestStdoutEmitter_ErrorSignal_DoesNotContainForbiddenTelemetryTokens(t *testing.T) {
	t.Parallel()

	var sink strings.Builder
	emitter := NewStdoutEmitter(&sink)
	emitter.EmitError(context.Background(), ErrorSignal{
		ResultClass:    "backend_error",
		ProviderFamily: "openai",
		Operation:      "responses.create",
		DurationBucket: "1_5s",
	})

	payload := strings.ToLower(sink.String()) // swobu:io-string source=domain
	for _, forbidden := range loadForbiddenTokensFixture(t) {
		if strings.Contains(payload, forbidden) {
			t.Fatalf("payload leaked forbidden token %q: %s", forbidden, sink.String())
		}
	}
}
