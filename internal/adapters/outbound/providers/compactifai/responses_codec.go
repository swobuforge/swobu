package compactifai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/provider"
	core "github.com/swobuforge/swobu/internal/wire/primitives"
)

type responsesCodec struct{ standard protocolcodec.Codec }

func (c responsesCodec) Encode(request provider.Request) (carrier.Document, []compat.Change, error) {
	return c.standard.Encode(request)
}

func (c responsesCodec) Decode(ctx context.Context, request provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	stream, ok := ingress.(provider.StreamIngress)
	if !ok {
		return c.standard.Decode(ctx, request, ingress)
	}
	stream.Stream.Body = newResponsesIdentityBody(stream.Stream.Body)
	return c.standard.Decode(ctx, request, stream)
}

type responsesIdentityBody struct {
	reader  *core.SSEReaderCloser
	buffer  bytes.Buffer
	itemIDs map[int]string
}

func newResponsesIdentityBody(body io.ReadCloser) *responsesIdentityBody {
	return &responsesIdentityBody{reader: core.NewSSEReader(body), itemIDs: make(map[int]string)}
}

func (b *responsesIdentityBody) Read(output []byte) (int, error) {
	for b.buffer.Len() == 0 {
		event, err := b.reader.Next(context.Background())
		if err != nil {
			return 0, err
		}
		data, err := b.normalize(event.Data)
		if err != nil {
			return 0, err
		}
		if event.Event != "" {
			fmt.Fprintf(&b.buffer, "event: %s\n", event.Event)
		}
		fmt.Fprintf(&b.buffer, "data: %s\n\n", data)
	}
	return b.buffer.Read(output)
}

func (b *responsesIdentityBody) normalize(data string) (string, error) {
	if data == "[DONE]" {
		return data, nil
	}
	var frame map[string]json.RawMessage
	if err := json.Unmarshal([]byte(data), &frame); err != nil {
		return "", err
	}
	var frameType string
	_ = json.Unmarshal(frame["type"], &frameType)
	switch frameType {
	case "response.output_item.added", "response.output_item.done":
		b.rememberOutputItem(frame)
	case "response.completed", "response.incomplete", "response.failed":
		if err := b.restoreTerminalOutputIDs(frame); err != nil {
			return "", err
		}
	}
	raw, err := json.Marshal(frame)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (b *responsesIdentityBody) rememberOutputItem(frame map[string]json.RawMessage) {
	var index int
	if err := json.Unmarshal(frame["output_index"], &index); err != nil {
		return
	}
	var item struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(frame["item"], &item) == nil && item.ID != "" {
		b.itemIDs[index] = item.ID
	}
}

func (b *responsesIdentityBody) restoreTerminalOutputIDs(frame map[string]json.RawMessage) error {
	var response map[string]json.RawMessage
	if err := json.Unmarshal(frame["response"], &response); err != nil {
		return err
	}
	var outputItems []map[string]json.RawMessage
	if err := json.Unmarshal(response["output"], &outputItems); err != nil {
		return nil
	}
	for index := range outputItems {
		if itemID := b.itemIDs[index]; itemID != "" {
			restored, err := json.Marshal(itemID)
			if err != nil {
				return err
			}
			outputItems[index]["id"] = restored
		}
	}
	raw, err := json.Marshal(outputItems)
	if err != nil {
		return err
	}
	response["output"] = raw
	raw, err = json.Marshal(response)
	if err != nil {
		return err
	}
	frame["response"] = raw
	return nil
}

func (b *responsesIdentityBody) Close() error { return b.reader.Close() }

var _ provider.Codec = responsesCodec{}
var _ io.ReadCloser = (*responsesIdentityBody)(nil)
