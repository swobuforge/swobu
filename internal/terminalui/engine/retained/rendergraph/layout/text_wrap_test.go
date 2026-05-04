package layout

import "testing"

func TestWrapLinePreserveIndent_WrapsLongProse(t *testing.T) {
	t.Parallel()
	line := "use for Other clients when OpenAI or Anthropic-compatible base URL is supported in this shell session"
	segments := WrapLinePreserveIndent(line, 40)
	if len(segments) < 2 {
		t.Fatalf("segments=%v want wrapped", segments)
	}
	for _, segment := range segments {
		if len([]rune(segment)) > 40 {
			t.Fatalf("segment too wide (%d): %q", len([]rune(segment)), segment)
		}
	}
}

func TestWrapLinePreserveIndent_PreservesLeadingIndentation(t *testing.T) {
	t.Parallel()
	line := "    apiBase: http://127.0.0.1:7926/c/acme/v1"
	segments := WrapLinePreserveIndent(line, 28)
	if len(segments) < 2 {
		t.Fatalf("segments=%v want wrapped", segments)
	}
	for _, segment := range segments {
		if len(segment) < 4 || segment[:4] != "    " {
			t.Fatalf("segment indentation lost: %q", segment)
		}
	}
}

func TestWrapLinePreserveIndent_SplitsLongUnbrokenToken(t *testing.T) {
	t.Parallel()
	line := "    OPENCODE_CONFIG_CONTENT={\"provider\":{\"swobu\":{\"options\":{\"baseURL\":\"http://127.0.0.1:7926/c/jobs/v1\"}}}}"
	segments := WrapLinePreserveIndent(line, 32)
	if len(segments) < 2 {
		t.Fatalf("segments=%v want wrapped", segments)
	}
	for _, segment := range segments {
		if len([]rune(segment)) > 32 {
			t.Fatalf("segment too wide (%d): %q", len([]rune(segment)), segment)
		}
		if len(segment) < 4 || segment[:4] != "    " {
			t.Fatalf("segment indentation lost: %q", segment)
		}
	}
}
