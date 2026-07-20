package messages

import (
	"context"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"io"
	"strings"
	"testing"
)

func TestDecodeResponseBufferedRejectsUnimplementedServerToolLifecycle(t *testing.T) {
	raw := []byte(`{"id":"msg_1","model":"m","content":[{"type":"server_tool_use","id":"s","name":"web_search","input":{"query":"x"}}]}`)
	if _, err := decodeResponseBuffered(context.Background(), canonical.CanonicalRequest{}, raw, "ex", nil); err == nil {
		t.Fatal("server tool lifecycle was silently dropped")
	}
}

func TestDecodeResponseStreamRejectsUnimplementedServerToolLifecycle(t *testing.T) {
	raw := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"m\"}}\n\n" + "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"server_tool_use\",\"id\":\"s\",\"name\":\"web_search\"}}\n\n"
	reader := decodeResponseStream(canonical.CanonicalRequest{}, carrier.ByteStream{MediaType: "text/event-stream", Body: io.NopCloser(strings.NewReader(raw))}, "ex", nil)
	if _, err := reader.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Next(context.Background()); err == nil {
		t.Fatal("server tool stream lifecycle was silently dropped")
	}
}
