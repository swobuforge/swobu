package responses

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestResponsesProjectionDebugLogsStructureWithoutContent(t *testing.T) {
	const canary = "SWOBU_PRIVATE_RESPONSE_CANARY"

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	logResponsesStreamProjectionFrame([]byte(`{"type":"response.output_text.delta","delta":"` + canary + `"}`))
	logResponsesStreamProjectionFrame([]byte(`{"type":"` + canary))

	got := logs.String()
	if strings.Contains(got, canary) {
		t.Fatalf("Responses output content reached logs: %s", got)
	}
	for _, structural := range []string{
		"component=protocol.responses",
		"event=responses_stream_projection_frame",
		"frame_bytes=",
	} {
		if !strings.Contains(got, structural) {
			t.Fatalf("logs missing structural field %q: %s", structural, got)
		}
	}
	if strings.Contains(got, "egress") {
		t.Fatalf("codec projection claimed client egress: %s", got)
	}
}
