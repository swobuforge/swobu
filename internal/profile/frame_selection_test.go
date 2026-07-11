package profile

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func SupportedFramesForSpecProtocol(spec string, protocolKind protocolkind.ProtocolKind) []string {
	if !SupportsExecutionProtocolForSpec(spec, protocolKind) {
		return nil
	}
	// Swobu v0 supports one batch frame and one response-stream frame.
	return []string{FrameHTTPJSONBody, FrameSSEEvent}
}

func DefaultFrameForSpecProtocol(spec string, protocolKind protocolkind.ProtocolKind) (string, bool) {
	supported := SupportedFramesForSpecProtocol(spec, protocolKind)
	if len(supported) == 0 {
		return "", false
	}
	// Prefer the highest-capability default frame for new provider configs.
	// Today that is server-sent events when available, with batch JSON as
	// deterministic fallback.
	preferred := []string{FrameSSEEvent, FrameHTTPJSONBody}
	for _, want := range preferred {
		for _, got := range supported {
			if got == want {
				return got, true
			}
		}
	}
	return supported[0], true
}

func TestDefaultFrameForSpecProtocol_PrefersSSEWhenSupported(t *testing.T) {
	t.Parallel()

	def, ok := DefaultFrameForSpecProtocol("openai", protocolkind.ChatCompletions)
	if !ok {
		t.Fatal("default frame missing for supported spec/protocol")
	}
	if def != FrameSSEEvent {
		t.Fatalf("default frame=%q want=%q", def, FrameSSEEvent)
	}
}

func TestDefaultFrameForSpecProtocol_UnsupportedSpecProtocol(t *testing.T) {
	t.Parallel()

	def, ok := DefaultFrameForSpecProtocol("anthropic", protocolkind.ChatCompletions)
	if ok {
		t.Fatalf("unexpected default frame=%q for unsupported spec/protocol", def)
	}
	if def != "" {
		t.Fatalf("default frame=%q want empty", def)
	}
}

func TestStreamingForFrame(t *testing.T) {
	t.Parallel()

	if streaming, ok := StreamingForFrame(FrameSSEEvent); !ok || !streaming {
		t.Fatalf("sse frame streaming=%v ok=%v", streaming, ok)
	}
	if streaming, ok := StreamingForFrame(FrameHTTPJSONBody); !ok || streaming {
		t.Fatalf("http-json frame streaming=%v ok=%v", streaming, ok)
	}
	if _, ok := StreamingForFrame("unknown"); ok {
		t.Fatal("unknown frame should be unsupported")
	}
}
