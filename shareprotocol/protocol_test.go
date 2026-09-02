package shareprotocol

import (
	"bytes"
	"strings"
	"testing"
)

func TestRetryAfterSecondsRoundTripsAndOmitsZero(t *testing.T) {
	var wire bytes.Buffer
	codec := NewCodec(&wire)
	if err := codec.Write(Message{Type: "error", Error: "later", RetryAfterSeconds: 21600}); err != nil {
		t.Fatal(err)
	}
	message, err := codec.Read()
	if err != nil {
		t.Fatal(err)
	}
	if message.RetryAfterSeconds != 21600 {
		t.Fatalf("retry_after_seconds = %d", message.RetryAfterSeconds)
	}

	wire.Reset()
	codec = NewCodec(&wire)
	if err := codec.Write(Message{Type: "error", Error: "later"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(wire.String(), "retry_after_seconds") {
		t.Fatalf("zero retry metadata was serialized: %s", wire.String())
	}
}
