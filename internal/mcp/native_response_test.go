package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestNativeResponseCaptureCorrelatesSSEAndBoundsMemory(t *testing.T) {
	capture := &nativeResponseCapture{mediaType: "text/event-stream", id: json.RawMessage(`7`), body: []byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\",\"params\":{}}\n\nevent: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":8,\"result\":{\"value\":0}}\n\nevent: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":7,\"result\":{\"value\":9007199254740993}}\n\n")}
	raw, err := capture.result(t.Context())
	if err != nil || string(raw) != `{"value":9007199254740993}` {
		t.Fatalf("wrong SSE response selected: %s %v", raw, err)
	}
	capture.body = bytes.Repeat([]byte("x"), MaxHTTPResponseBytes)
	body := &nativeResponseBody{ReadCloser: io.NopCloser(strings.NewReader("overflow")), capture: capture}
	if _, err := body.Read(make([]byte, 8)); err == nil || len(capture.body) != MaxHTTPResponseBytes {
		t.Fatal("capture exceeded bounded memory")
	}
	other := &nativeResponseCapture{mediaType: "application/json", id: json.RawMessage(`9`), body: []byte(`{"jsonrpc":"2.0","id":10,"result":{"value":1}}`)}
	if raw, err := other.result(t.Context()); err != nil || len(raw) != 0 {
		t.Fatal("mismatched response identity supplied native data")
	}
}
