package carrier

import (
	"io"
	"strings"
	"testing"
)

func TestByteStreamExposesRawBodyWithoutInventedFrames(t *testing.T) {
	body := io.NopCloser(strings.NewReader("abcdef"))
	stream := ByteStream{Body: body}

	raw, err := io.ReadAll(stream.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(raw) != "abcdef" {
		t.Fatalf("body = %q, want abcdef", string(raw))
	}
}
