package messages

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

const defaultMessagesMaxTokens = 256

type messageBody struct {
	Role    string      `json:"role"`
	Content []contentID `json:"content"`
}

type contentID struct {
	Type      string                `json:"type"`
	Text      string                `json:"text,omitempty"`
	ID        string                `json:"id,omitempty"`
	Name      string                `json:"name,omitempty"`
	Input     json.RawMessage       `json:"input,omitempty"`
	ToolUseID string                `json:"tool_use_id,omitempty"`
	IsError   bool                  `json:"is_error,omitempty"`
	Content   any                   `json:"content,omitempty"`
	Source    any                   `json:"source,omitempty"`
	Thinking  *string               `json:"thinking,omitempty"`
	Signature string                `json:"signature,omitempty"`
	Data      string                `json:"data,omitempty"`
	Citations []messagesCitationDTO `json:"citations,omitempty"`
}

// EncodeOptions selects destination-specific image behavior while keeping the
// reusable Messages grammar independent of provider identity.
type EncodeOptions struct {
	Compatibility compat.CompatibilityPolicy
}

// ProviderRequestDocument is the standard Messages lowering before an exact
// provider owns any typed dialect adaptation.
type ProviderRequestDocument struct {
	Payload map[string]any
	Tools   []ProviderRequestTool
}

func EncodeCarrierWithDecisions(req canonical.CanonicalRequest, d delivery.Delivery, sink compat.Sink, exchangeID string, options EncodeOptions) (carrier.Document, error) {
	document, err := LowerProviderRequestDocument(req, d, sink, exchangeID, options)
	if err != nil {
		return carrier.Document{}, err
	}
	return EncodeProviderRequestDocument(document)
}

// LowerProviderRequestDocument produces a typed Messages document without
// crossing the JSON boundary.
func LowerProviderRequestDocument(req canonical.CanonicalRequest, d delivery.Delivery, sink compat.Sink, exchangeID string, options EncodeOptions) (ProviderRequestDocument, error) {
	switch d.Mode {
	case delivery.Buffered, delivery.Streaming:
	default:
		return ProviderRequestDocument{}, canonical.UnsupportedDelivery("conversation requests do not implement the requested delivery mode on the messages protocol")
	}
	items := req.Items()
	items, projectionDecisions, err := projectMessagesWebSearchLifecycles(items, compat.RequestItemsKind)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	if err := commitMessagesProjectionDecisions(sink, exchangeID, projectionDecisions); err != nil {
		return ProviderRequestDocument{}, err
	}
	tools := req.Tools()
	wireMessages, err := encodeItems(items, tools, sink, exchangeID, options)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	payload := map[string]any{
		"model":    req.Model(),
		"messages": wireMessages,
	}
	loweredInstructions := flattenInstructionsForMessages(req.Instructions())
	if err := commitMessagesInstructionDecisions(sink, exchangeID, loweredInstructions); err != nil {
		return ProviderRequestDocument{}, err
	}
	if loweredInstructions.Text != "" {
		payload["system"] = loweredInstructions.Text
	}
	wireTools, err := encodeMessagesTools(tools, sink, exchangeID, options)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	if err := encodeMessagesGenerationControls(payload, req.Controls(), req.Reasoning()); err != nil {
		return ProviderRequestDocument{}, err
	}
	if err := encodeMessagesReasoning(payload, req.Reasoning()); err != nil {
		if decisionErr := emitMessagesDecision(sink, exchangeID, compat.RequestReasoning, compat.Reject); decisionErr != nil {
			return ProviderRequestDocument{}, decisionErr
		}
		return ProviderRequestDocument{}, err
	}
	if err := rejectMessagesOutputFormat(req.OutputFormat()); err != nil {
		if decisionErr := emitMessagesDecision(sink, exchangeID, compat.RequestOutputFormat, compat.Reject); decisionErr != nil {
			return ProviderRequestDocument{}, decisionErr
		}
		return ProviderRequestDocument{}, err
	}
	choice, err := encodeMessagesToolChoice(req.EffectiveToolPolicy(), tools, sink, exchangeID)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	choice, err = encodeMessagesToolCallBatch(choice, req.ToolCallBatch(), len(tools) > 0)
	if err != nil {
		return ProviderRequestDocument{}, err
	}
	if choice != nil {
		payload["tool_choice"] = choice
	}
	if d.Mode == delivery.Streaming {
		payload["stream"] = true
	}
	return ProviderRequestDocument{Payload: payload, Tools: wireTools}, nil
}

// EncodeProviderRequestDocument performs the single serialization boundary
// after standard lowering or exact-provider typed composition.
func EncodeProviderRequestDocument(document ProviderRequestDocument) (carrier.Document, error) {
	if len(document.Tools) > 0 {
		document.Payload["tools"] = document.Tools
	} else {
		delete(document.Payload, "tools")
	}
	raw, err := json.Marshal(document.Payload)
	if err != nil {
		return carrier.Document{}, canonical.BadRequest("conversation request could not be encoded for the messages protocol")
	}
	return carrier.NewDocument(
		"",
		"application/json",
		nil,
		raw,
		carrier.Meta{},
	), nil
}

func encodeItems(items []canonical.CanonicalItem, tools []canonical.ToolDeclaration, sink compat.Sink, exchangeID string, options EncodeOptions) ([]messageBody, error) {
	if len(items) == 0 {
		return nil, canonical.BadRequest("messages protocol requires at least one canonical item")
	}
	out := make([]messageBody, 0, len(items))
	for i := 0; i < len(items); {
		owner := items[i].Owner()
		if owner != canonical.TurnOwnerUser && owner != canonical.TurnOwnerAssistant {
			return nil, canonical.UnsupportedOperation("messages protocol cannot lower interleaved system or developer messages")
		}
		wire := messageBody{Role: string(owner)}
		for i < len(items) && items[i].Owner() == owner {
			var err error
			wire.Content, err = appendMessagesItemBlocks(wire.Content, items[i], tools, owner, sink, exchangeID, options)
			if err != nil {
				return nil, err
			}
			i++
		}
		if len(wire.Content) > 0 {
			out = append(out, wire)
		}
	}
	return out, nil
}

func appendMessagesItemBlocks(blocks []contentID, item canonical.CanonicalItem, tools []canonical.ToolDeclaration, owner canonical.TurnOwner, sink compat.Sink, exchangeID string, options EncodeOptions) ([]contentID, error) {
	if message, ok := item.Message(); ok {
		for _, part := range message.Content() {
			if text, ok := part.Text(); ok {
				citations, err := encodeMessagesCitations(text.Text(), part.Citations())
				if err != nil {
					return nil, err
				}
				blocks = append(blocks, contentID{Type: "text", Text: text.Text(), Citations: citations})
				continue
			}
			if owner != canonical.TurnOwnerUser {
				return nil, canonical.UnsupportedOperation("messages protocol only accepts image input in user messages")
			}
			image, ok := part.Image()
			if !ok {
				return nil, canonical.UnsupportedOperation("messages protocol cannot lower this content kind")
			}
			block, err := encodeMessagesImage(image, sink, exchangeID, options, compat.RequestItemsMessageImageDetail)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
		}
		return blocks, nil
	}
	if item.Kind() == canonical.ItemKindToolCall {
		block, err := encodeMessagesToolCall(item, tools)
		if err != nil {
			return nil, err
		}
		return append(blocks, block), nil
	}
	if result, ok := item.ToolResult(); ok {
		if search, ok := result.WebSearch(); ok {
			content, err := encodeMessagesWebSearchResult(search)
			if err != nil {
				return nil, err
			}
			return append(blocks, contentID{Type: "web_search_tool_result", ToolUseID: result.CallID().String(), Content: content}), nil
		}
		content, err := encodeMessagesToolResultContent(result.Content(), sink, exchangeID, options)
		if err != nil {
			return nil, err
		}
		return append(blocks, contentID{Type: "tool_result", ToolUseID: result.CallID().String(), Content: content, IsError: result.IsError()}), nil
	}
	if reasoning, ok := item.Reasoning(); ok {
		opaque, exact := reasoning.Opaque().Messages()
		if !exact {
			return blocks, nil
		}
		var block contentID
		if err := json.Unmarshal(opaque, &block); err != nil || block.Type != "thinking" && block.Type != "redacted_thinking" {
			return nil, canonical.InternalError("messages opaque thinking is invalid")
		}
		return append(blocks, block), nil
	}
	return nil, canonical.UnsupportedOperation("canonical item is not supported on the messages protocol")
}

func encodeMessagesToolCall(item canonical.CanonicalItem, _ []canonical.ToolDeclaration) (contentID, error) {
	call, ok := item.ToolCall()
	if !ok {
		return contentID{}, canonical.InternalError("messages tool-call item is invalid")
	}
	tool := call.Tool()
	if tool.Kind() == canonical.ToolKindWebSearch {
		search, ok := call.Input().WebSearch()
		if !ok || search.Action != canonical.WebSearchActionSearch || len(search.Queries) != 1 {
			return contentID{}, canonical.UnsupportedOperation("messages protocol requires one search query per server-tool call")
		}
		input, err := json.Marshal(map[string]string{"query": search.Queries[0]})
		if err != nil {
			return contentID{}, canonical.InternalError("messages web-search call could not be encoded")
		}
		return contentID{Type: "server_tool_use", ID: call.CallID().String(), Name: "web_search", Input: input}, nil
	}
	if tool.Kind() != canonical.ToolKindFunction {
		return contentID{}, canonical.UnsupportedOperation("messages protocol tool-call kind is unsupported")
	}
	name := tool.Name()
	object, ok := call.Input().Object()
	if !ok {
		return contentID{}, canonical.BadRequest("messages function tool calls require object input")
	}
	return contentID{Type: "tool_use", ID: call.CallID().String(), Name: name, Input: json.RawMessage(object.Bytes())}, nil
}

func messagesTextOnlyContent(parts []canonical.MessagePart, surface string) (string, error) {
	var builder strings.Builder
	for _, part := range parts {
		text, ok := part.Text()
		if !ok {
			return "", canonical.UnsupportedOperation(surface + " do not support this content kind")
		}
		builder.WriteString(text.Text())
	}
	return builder.String(), nil
}

func encodeMessagesToolResultContent(parts []canonical.ToolResultPart, sink compat.Sink, exchangeID string, options EncodeOptions) (any, error) {
	if len(parts) == 1 {
		if text, ok := parts[0].Text(); ok {
			return text.Text(), nil
		}
	}
	content := make([]contentID, 0, len(parts))
	for _, part := range parts {
		if text, ok := part.Text(); ok {
			content = append(content, contentID{Type: "text", Text: text.Text()})
			continue
		}
		image, ok := part.Image()
		if !ok {
			return nil, canonical.UnsupportedOperation("messages tool results do not support this content kind")
		}
		block, err := encodeMessagesImage(image, sink, exchangeID, options, compat.RequestItemsToolResultImageDetail)
		if err != nil {
			return nil, err
		}
		content = append(content, block)
	}
	return content, nil
}

func encodeMessagesImage(image canonical.ImagePart, sink compat.Sink, exchangeID string, options EncodeOptions, detailFeature compat.Feature) (contentID, error) {
	if image.Detail().IsSpecified() {
		if options.Compatibility.EffectiveMode() == compat.CompatibilityStrict {
			if err := emitMessagesImageDecision(sink, exchangeID, detailFeature, compat.Reject); err != nil {
				return contentID{}, err
			}
			return contentID{}, canonical.UnsupportedOperation("messages protocol cannot lower an image detail preference")
		}
		if err := emitMessagesImageDecision(sink, exchangeID, detailFeature, compat.Approx); err != nil {
			return contentID{}, err
		}
	}
	source := image.Source()
	if rawURL, ok := source.URL(); ok {
		return contentID{Type: "image", Source: map[string]string{"type": "url", "url": rawURL.String()}}, nil
	}
	if inline, ok := source.Inline(); ok {
		return contentID{Type: "image", Source: map[string]string{"type": "base64", "media_type": string(inline.MediaType()), "data": base64.StdEncoding.EncodeToString(inline.Data())}}, nil
	}
	return contentID{}, canonical.InternalError("canonical image source is invalid")
}

func emitMessagesImageDecision(sink compat.Sink, exchangeID string, feature compat.Feature, outcome compat.Outcome) error {
	return emitMessagesDecision(sink, exchangeID, feature, outcome)
}

func emitMessagesDecision(sink compat.Sink, exchangeID string, feature compat.Feature, outcome compat.Outcome) error {
	if sink == nil {
		return nil
	}
	if err := sink.Commit(context.Background(), exchangeID, []compat.Decision{{
		Feature: feature,
		Outcome: outcome,
		Subject: compat.Subject("canonical:" + string(feature)),
	}}); err != nil {
		return canonical.InternalError("compatibility decision sink commit failed")
	}
	return nil
}

func encodeMessagesTools(tools []canonical.ToolDeclaration, sink compat.Sink, exchangeID string, options EncodeOptions) ([]ProviderRequestTool, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	for _, tool := range tools {
		if decl, ok := tool.Function(); ok {
			if strict, specified := decl.Strict().Get(); specified && strict {
				if err := emitMessagesDecision(sink, exchangeID, compat.RequestToolsSchemaStrict, compat.Drop); err != nil {
					return nil, err
				}
				break
			}
		}
	}
	out := make([]ProviderRequestTool, 0, len(tools))
	for _, tool := range tools {
		if decl, ok := tool.Function(); ok {
			wire, err := encodeMessagesFunctionTool(tool, decl)
			if err != nil {
				return nil, err
			}
			out = append(out, wire)
			continue
		}
		if tool.Kind() == canonical.ToolKindWebSearch {
			out = append(out, ProviderRequestTool{Type: "web_search", Name: "web_search"})
			continue
		}
		return nil, canonical.UnsupportedOperation("messages protocol does not support this tool declaration")
	}
	return out, nil
}

func encodeMessagesFunctionTool(declaration canonical.ToolDeclaration, decl canonical.FunctionTool) (ProviderRequestTool, error) {
	schema, err := messagesToolSchema(decl.InputSchema())
	if err != nil {
		return ProviderRequestTool{}, err
	}
	name := declaration.Key().Name()
	name = strings.TrimSpace(name) // swobu:io-string source=boundary
	if name == "" {
		return ProviderRequestTool{}, canonical.BadRequest("messages protocol tool declarations require a name")
	}
	return ProviderRequestTool{
		Name:        name,
		Description: strings.TrimSpace(decl.Description()), // swobu:io-string source=boundary
		InputSchema: schema,
	}, nil
}

func messagesToolSchema(schema canonical.ToolSchema) (json.RawMessage, error) {
	raw := strings.TrimSpace(schema.RawObject()) // swobu:io-string source=domain
	if raw == "" {
		return nil, canonical.BadRequest("messages protocol tool declarations require input_schema")
	}
	obj, err := sse.DecodeJSONObject(json.RawMessage(raw), "messages protocol tool declaration input_schema is invalid")
	if err != nil {
		return nil, err
	}
	normalized, err := json.Marshal(obj)
	if err != nil {
		return nil, canonical.InternalError("messages protocol tool declarations could not be encoded")
	}
	return json.RawMessage(normalized), nil
}

func messagesToolSchemaFromWire(raw json.RawMessage) (canonical.ToolSchema, error) {
	trimmed := strings.TrimSpace(string(raw)) // swobu:io-string source=domain
	if trimmed == "" || trimmed == "null" {
		return canonical.ToolSchema{}, canonical.BadRequest("messages request tool declarations require input_schema")
	}
	obj, err := sse.DecodeJSONObject(json.RawMessage(trimmed), "messages request tool declaration input_schema is invalid")
	if err != nil {
		return canonical.ToolSchema{}, err
	}
	normalized, err := json.Marshal(obj)
	if err != nil {
		return canonical.ToolSchema{}, canonical.InternalError("messages request tool declarations could not be decoded")
	}
	object, err := canonical.ParseJSONObject(normalized)
	if err != nil {
		return canonical.ToolSchema{}, canonical.BadRequest("messages request tool declaration input_schema is invalid")
	}
	return canonical.NewToolSchemaObject(object), nil
}
