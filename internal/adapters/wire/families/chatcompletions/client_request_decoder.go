package chatcompletions

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	sse "github.com/swobuforge/swobu/internal/adapters/wire/framing/sse"
	core "github.com/swobuforge/swobu/internal/adapters/wire/primitives"
	openaicompat "github.com/swobuforge/swobu/internal/adapters/wire/shared/openaicompat"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

type ClientRequestDecoder struct{}
type ClientDocumentEncoder struct{}
type ClientStreamEncoder struct{}
type ProviderRequestEncoder struct{}
type ProviderDocumentDecoder struct{}
type ProviderStreamDecoder struct{}

func (ClientRequestDecoder) DecodeClientRequest(doc carrier.WireDocument) (canonical.CanonicalRequest, delivery.Delivery, error) {
	var dto chatCompletionsRequestDTO
	if err := json.Unmarshal(doc.Raw, &dto); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("chat completions request body is invalid JSON")
	}
	if len(dto.Messages) == 0 {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("chat completions request is missing required fields")
	}
	items := make([]canonical.CanonicalItem, 0, len(dto.Messages))
	for idx, msg := range dto.Messages {
		decoded, err := decodeChatCompletionsItems(msg.Role, msg.Content, msg.ToolCalls, msg.ToolCallID, idx)
		if err != nil {
			return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), err
		}
		items = append(items, decoded...)
	}
	resolvedDelivery := delivery.BufferedDelivery()
	if dto.Stream {
		resolvedDelivery = delivery.StreamingDelivery(delivery.FramingNone)
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: strings.TrimSpace(dto.Model), // swobu:io-string source=boundary
		Items: items,
	}), resolvedDelivery, nil
}

func (ClientDocumentEncoder) EncodeClientDocument(output canonical.CanonicalOutput) (carrier.WireDocument, error) {
	raw, err := json.Marshal(chatCompletionsResponseDTO{
		ID:     sse.FallbackID(output.ResultID(), "chatcmpl_swobu"),
		Object: "chat.completion",
		Model:  output.Model(),
		Choices: []chatCompletionsChoiceDTO{{
			Index:        0,
			Message:      chatMessageFromOutput(output),
			FinishReason: sse.DefaultFinishReason(output.FinishReason(), "stop"),
		}},
		Usage: chatUsageFromCanonical(output.Usage()),
	})
	if err != nil {
		return carrier.WireDocument{}, err
	}
	return carrier.WireDocument{Family: protocolkind.ChatCompletions, Media: "application/json", Raw: raw}, nil
}

func (ClientStreamEncoder) newStreamState() sse.EnvelopeStreamEncoder {
	return &chatCompletionsEnvelopeStreamEncoder{adapter: sse.NewEnvelopeEventAdapter()}
}

func (e ClientStreamEncoder) EncodeClientStream(events canonical.EventReader) (carrier.WireStream, error) {
	state := e.newStreamState()
	pr, pw := io.Pipe()
	go func() {
		defer func() { _ = events.Close(context.Background()) }()
		defer func() { _ = pw.Close() }()
		for {
			ev, err := events.Next(context.Background())
			if err != nil {
				if errors.Is(err, io.EOF) {
					return
				}
				_ = pw.CloseWithError(err)
				return
			}
			frames, err := state.EncodeEnvelopeEvent(ev)
			if err != nil {
				_ = pw.CloseWithError(err)
				return
			}
			for _, frame := range frames {
				if _, err := pw.Write(frame); err != nil {
					_ = pw.CloseWithError(err)
					return
				}
			}
		}
	}()
	return carrier.WireStream{Family: protocolkind.ChatCompletions, Framing: carrier.FramingSSE, Frames: carrier.FrameReaderFromReadCloser(pr)}, nil
}

func (ProviderRequestEncoder) EncodeProviderRequest(request canonical.CanonicalRequest, delivery delivery.Delivery) (carrier.WireDocument, error) {
	return encodeRequestCarrier(request, delivery)
}

func (ProviderDocumentDecoder) DecodeProviderDocument(doc carrier.WireDocument, exchangeID string) (canonical.EventReader, error) {
	return decodeResponseBuffered(doc.Raw, exchangeID)
}

func (ProviderStreamDecoder) DecodeProviderStream(stream carrier.WireStream, exchangeID string) canonical.EventReader {
	if err := core.ValidateResponseSSECarrierStream(stream, protocolkind.ChatCompletions); err != nil {
		carrierErr := canonical.InternalError("chat completions stream wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_stream_invariant": err.Error()}
		return canonical.NewErrorEventReader(carrierErr)
	}
	return decodeResponseStream(stream, exchangeID)
}

func decodeChatCompletionsItems(
	role string,
	content json.RawMessage,
	toolCalls []chatCompletionsToolCallDTO,
	toolCallID string,
	msgIdx int,
) ([]canonical.CanonicalItem, error) {
	author := openaicompat.AuthorForRole(role)
	textItems, err := openaicompat.DecodeTextContentItems(content, "chat completions", author)
	if err != nil {
		return nil, err
	}
	role = strings.TrimSpace(role) // swobu:io-string source=boundary
	if role == "tool" {
		if strings.TrimSpace(toolCallID) == "" { // swobu:io-string source=boundary
			return nil, canonical.BadRequest("chat completions tool messages require tool_call_id")
		}
		return []canonical.CanonicalItem{
			canonical.NewToolResultItem(canonical.ItemAuthorTool, strings.TrimSpace(toolCallID), joinItemText(textItems)), // swobu:io-string source=boundary
		}, nil
	}
	items := append([]canonical.CanonicalItem(nil), textItems...)
	for idx, call := range toolCalls {
		if call.Type != "" && call.Type != "function" {
			return nil, canonical.BadRequest("chat completions request contains an unsupported tool call type")
		}
		if strings.TrimSpace(call.Function.Name) == "" { // swobu:io-string source=boundary
			return nil, canonical.BadRequest("chat completions tool calls require a function name")
		}
		input, err := sse.DecodeJSONObject(call.Function.Arguments, "chat completions tool call arguments are invalid")
		if err != nil {
			return nil, err
		}
		id := strings.TrimSpace(call.ID) // swobu:io-string source=boundary
		if id == "" {
			id = openaicompat.GeneratedToolUseID(msgIdx, idx)
		}
		items = append(items, canonical.NewToolUseItem(author, "", id, strings.TrimSpace(call.Function.Name), input)) // swobu:io-string source=boundary
	}
	return items, nil
}

func joinItemText(items []canonical.CanonicalItem) string {
	var builder strings.Builder
	for _, item := range items {
		if item.Kind != canonical.ItemKindText {
			continue
		}
		builder.WriteString(item.Text)
	}
	return builder.String()
}
