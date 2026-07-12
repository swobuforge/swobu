package responses

import (
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodeResponseBuffered_RejectsReasoningOutput(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"id":"resp_1",
		"model":"m",
		"output":[{"type":"reasoning","id":"reasoning_1"}]
	}`)

	_, err := decodeResponseBuffered(raw, "ex_reasoning")
	if err == nil {
		t.Fatal("expected decodeResponseBuffered to reject reasoning output")
	}
	var compatErr canonical.Error
	if !errors.As(err, &compatErr) {
		t.Fatalf("expected canonical.Error, got %T", err)
	}
	if compatErr.Code != canonical.ErrorCodeUnsupportedOperation {
		t.Fatalf("error code = %q, want %q", compatErr.Code, canonical.ErrorCodeUnsupportedOperation)
	}
	if !strings.Contains(compatErr.Message, "reasoning") {
		t.Fatalf("error message = %q, want reasoning to be mentioned", compatErr.Message)
	}
}
