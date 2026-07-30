package responses

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestResponsesEgressDebugLogsStructureWithoutContent(t *testing.T) {
	const canary = "SWOBU_PRIVATE_RESPONSE_CANARY"

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	logResponsesEgressBuffered([]byte(`{"output_text":"` + canary + `"}`))
	logResponsesEgressStreamFrame([]byte(`{"type":"response.output_text.delta","delta":"` + canary + `"}`))
	logResponsesEgressStreamFrame([]byte(`{"type":"` + canary))

	got := logs.String()
	if strings.Contains(got, canary) {
		t.Fatalf("Responses output content reached logs: %s", got)
	}
	for _, structural := range []string{
		"event=responses_buffered_egress",
		"payload_bytes=",
		"event=responses_stream_egress_frame",
		"frame_bytes=",
	} {
		if !strings.Contains(got, structural) {
			t.Fatalf("logs missing structural field %q: %s", structural, got)
		}
	}
}
