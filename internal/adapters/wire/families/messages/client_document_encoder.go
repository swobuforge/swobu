package messages

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
	var dto messagesRequestDTO
	if err := json.Unmarshal(doc.Raw, &dto); err != nil {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("messages request body is invalid JSON")
	}
	if len(dto.Messages) == 0 {
		return canonical.CanonicalRequest{}, delivery.BufferedDelivery(), canonical.BadRequest("messages request is missing required fields")
	}
	items := make([]canonical.CanonicalItem, 0, len(dto.Messages))
	for idx, msg := range dto.Messages {
		decoded, err := decodeMessagesItems(msg.Content, idx, strings.TrimSpace(msg.Role)) // swobu:io-string source=boundary
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
	items := output.Items()
	content := make([]messagesResponsePartDTO, 0, len(items))
	for _, item := range items {
		switch item.Kind {
		case canonical.ItemKindText:
			content = append(content, messagesResponsePartDTO{Type: "text", Text: item.Text})
		case canonical.ItemKindToolUse:
			content = append(content, messagesResponsePartDTO{
				Type:  "tool_use",
				ID:    item.ToolUseID,
				Name:  item.Name,
				Input: item.Input,
			})
		}
	}
	stopReason := "end_turn"
	if sse.ContainsToolUseOutput(items) {
		stopReason = "tool_use"
	}
	raw, err := json.Marshal(messagesResponseDTO{
		ID:         sse.FallbackID(output.ResultID(), "msg_swobu"),
		Type:       "message",
		Role:       "assistant",
		Model:      output.Model(),
		Content:    content,
		StopReason: stopReason,
		Usage:      messagesUsageFromCanonical(output.Usage()),
	})
	if err != nil {
		return carrier.WireDocument{}, err
	}
	return carrier.WireDocument{Family: protocolkind.Messages, Media: "application/json", Raw: raw}, nil
}

func (ClientStreamEncoder) newStreamState() sse.EnvelopeStreamEncoder {
	return &messagesEnvelopeStreamEncoder{adapter: sse.NewEnvelopeEventAdapter()}
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
	return carrier.WireStream{Family: protocolkind.Messages, Framing: carrier.FramingSSE, Frames: carrier.FrameReaderFromReadCloser(pr)}, nil
}

func (ProviderRequestEncoder) EncodeProviderRequest(request canonical.CanonicalRequest, delivery delivery.Delivery) (carrier.WireDocument, error) {
	return encodeRequestCarrier(request, delivery)
}

func (ProviderDocumentDecoder) DecodeProviderDocument(doc carrier.WireDocument, exchangeID string) (canonical.EventReader, error) {
	return decodeResponseBuffered(doc.Raw, exchangeID)
}

func (ProviderStreamDecoder) DecodeProviderStream(stream carrier.WireStream, exchangeID string) canonical.EventReader {
	if err := core.ValidateResponseSSECarrierStream(stream, protocolkind.Messages); err != nil {
		carrierErr := canonical.InternalError("messages stream wire carrier is invalid")
		carrierErr.Details = map[string]string{"wire_stream_invariant": err.Error()}
		return canonical.NewErrorEventReader(carrierErr)
	}
	return decodeResponseStream(stream, exchangeID)
}

func decodeMessagesItems(raw json.RawMessage, msgIdx int, role string) ([]canonical.CanonicalItem, error) {
	_ = msgIdx
	if role == "" {
		role = "user"
	}
	author := openaicompat.AuthorForRole(role)
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if text != "" {
			return []canonical.CanonicalItem{canonical.NewTextItem(author, text)}, nil
		}
		return nil, canonical.BadRequest("messages request content must not be empty")
	}
	var parts []messagesContentPartDTO
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, canonical.BadRequest("messages request content is invalid")
	}
	if len(parts) == 0 {
		return nil, canonical.BadRequest("messages request content must not be empty")
	}
	decoded := make([]canonical.CanonicalItem, 0, len(parts))
	for _, part := range parts {
		partType := strings.TrimSpace(part.Type) // swobu:io-string source=provider-wire
		switch partType {
		case "text":
			if part.Text == "" {
				return nil, canonical.BadRequest("messages request text parts must not be empty")
			}
			decoded = append(decoded, canonical.NewTextItem(author, part.Text))
		case "tool_use":
			if strings.TrimSpace(part.Name) == "" { // swobu:io-string source=boundary
				return nil, canonical.BadRequest("messages request tool_use parts require a name")
			}
			input, err := sse.DecodeJSONObject(part.Input, "messages request tool_use input is invalid")
			if err != nil {
				return nil, err
			}
			decoded = append(decoded, canonical.NewToolUseItem(author, "", strings.TrimSpace(part.ID), strings.TrimSpace(part.Name), input)) // swobu:io-string source=boundary
		case "tool_result":
			if strings.TrimSpace(part.ToolUseID) == "" { // swobu:io-string source=boundary
				return nil, canonical.BadRequest("messages request tool_result parts require tool_use_id")
			}
			text, err := decodeToolResultText(part.Content)
			if err != nil {
				return nil, err
			}
			decoded = append(decoded, canonical.NewToolResultItem(author, strings.TrimSpace(part.ToolUseID), text)) // swobu:io-string source=boundary
		default:
			return nil, canonical.BadRequest("messages request content contains an unsupported part type")
		}
	}
	return decoded, nil
}

func decodeToolResultText(raw json.RawMessage) (string, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}
	var parts []messagesTextPartDTO
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", canonical.BadRequest("messages request tool_result content is invalid")
	}
	var builder strings.Builder
	for _, part := range parts {
		if strings.TrimSpace(part.Type) != "text" { // swobu:io-string source=boundary
			return "", canonical.BadRequest("messages request tool_result content must contain text parts only")
		}
		builder.WriteString(part.Text)
	}
	return builder.String(), nil
}

func messagesUsageFromCanonical(usage canonical.TokenUsage) *messagesUsageDTO {
	input, hasInput := usage.InputTokens()
	output, hasOutput := usage.OutputTokens()
	cacheRead, hasCacheRead := usage.CacheReadTokens()
	cacheWrite, hasCacheWrite := usage.CacheWriteTokens()
	if !hasInput && !hasOutput && !hasCacheRead && !hasCacheWrite {
		return nil
	}
	return &messagesUsageDTO{
		InputTokens:              input,
		OutputTokens:             output,
		CacheReadInputTokens:     cacheRead,
		CacheCreationInputTokens: cacheWrite,
	}
}
