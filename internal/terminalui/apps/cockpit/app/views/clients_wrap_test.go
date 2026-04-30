package views

import (
	"testing"

	"github.com/metrofun/swobu/internal/terminalui/engine/retained/rendergraph/layout"
)

func TestWrapPayloadLine_WrapsLongProseWithoutEllipsis(t *testing.T) {
	t.Parallel()
	line := "use for Other clients when OpenAI or Anthropic-compatible base URL is supported in this shell session"
	segments := layout.WrapLinePreserveIndent(line, 40)
	if len(segments) < 2 {
		t.Fatalf("segments=%v want wrapped", segments)
	}
	for _, segment := range segments {
		if len([]rune(segment)) > 40 {
			t.Fatalf("segment too wide (%d): %q", len([]rune(segment)), segment)
		}
	}
}

func TestWrapPayloadLine_PreservesLeadingIndentation(t *testing.T) {
	t.Parallel()
	line := "    apiBase: http://127.0.0.1:7777/c/acme/v1"
	segments := layout.WrapLinePreserveIndent(line, 28)
	if len(segments) < 2 {
		t.Fatalf("segments=%v want wrapped", segments)
	}
	for _, segment := range segments {
		if len(segment) < 4 || segment[:4] != "    " {
			t.Fatalf("segment indentation lost: %q", segment)
		}
	}
}

func TestDisclosureNoteRows_WrapsLongBackendErrors(t *testing.T) {
	t.Parallel()
	rows := DisclosureNoteRows(`backend error from draft (401): { "error": { "message": "Incorrect API key provided: sk-or-v1********************" } }`)
	if len(rows) < 2 {
		t.Fatalf("rows=%d want wrapped disclosure output", len(rows))
	}
}

func TestWrappedPayloadTextRows_WrapsLongUnbrokenClientCommand(t *testing.T) {
	t.Parallel()
	rows := wrappedPayloadTextRows(`OPENCODE_CONFIG_CONTENT={"provider":{"swobu":{"options":{"baseURL":"http://127.0.0.1:7777/c/jobs/v1"}}}}`)
	if len(rows) < 2 {
		t.Fatalf("rows=%d want wrapped payload output", len(rows))
	}
}
