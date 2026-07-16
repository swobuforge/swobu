package messages

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/effect"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

const defaultMessagesMaxTokens = 256

type messageBody struct {
	Role    string      `json:"role"`
	Content []contentID `json:"content"`
}

type contentID struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   string         `json:"content,omitempty"`
}

func EncodeCarrierWithEffects(req canonical.CanonicalRequest, d delivery.Delivery, sink effect.Sink, exchangeID string) (carrier.CarrierDocument, error) {
	switch d.Mode {
	case delivery.Buffered, delivery.Streaming:
	default:
		return carrier.CarrierDocument{}, canonical.UnsupportedDelivery("conversation requests do not implement the requested delivery mode on the messages protocol")
	}
	items := req.Items()
	tools := req.Tools()
	wireMessages, err := encodeItems(items)
	if err != nil {
		return carrier.CarrierDocument{}, err
	}
	payload := map[string]any{
		"model":    req.Model(),
		"messages": wireMessages,
	}
	if instructions := strings.TrimSpace(req.Instructions()); instructions != "" { // swobu:io-string source=boundary
		payload["system"] = instructions
	}
	if wireTools, err := encodeMessagesTools(tools, sink, exchangeID); err != nil {
		return carrier.CarrierDocument{}, err
	} else if len(wireTools) > 0 {
		payload["tools"] = wireTools
	}
	if err := encodeMessagesGenerationControls(payload, req.Controls()); err != nil {
		return carrier.CarrierDocument{}, err
	}
	if err := rejectMessagesOutputFormat(req.OutputFormat()); err != nil {
		return carrier.CarrierDocument{}, err
	}
	choice, err := encodeMessagesToolChoice(req.ToolPolicy(), tools, sink, exchangeID)
	if err != nil {
		return carrier.CarrierDocument{}, err
	}
	choice, err = encodeMessagesToolCallBatch(choice, req.ToolCallBatch(), len(tools) > 0)
	if err != nil {
		return carrier.CarrierDocument{}, err
	}
	if choice != nil {
		payload["tool_choice"] = choice
	}
	if d.Mode == delivery.Streaming {
		payload["stream"] = true
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return carrier.CarrierDocument{}, canonical.BadRequest("conversation request could not be encoded for the messages protocol")
	}
	return carrier.NewCarrierDocument(
		carrier.StageProviderRequestOut,
		"",
		"application/json",
		nil,
		raw,
		carrier.Meta{},
	), nil
}

func encodeItems(items []canonical.CanonicalItem) ([]messageBody, error) {
	if len(items) == 0 {
		return nil, canonical.BadRequest("messages protocol requires at least one canonical item")
	}
	out := make([]messageBody, 0, len(items))
	toolTypes := map[string]string{}
	for i := 0; i < len(items); {
		role := roleForMessagesItem(items[i])
		content := make([]contentID, 0, 1)
		for i < len(items) && roleForMessagesItem(items[i]) == role {
			current := items[i]
			switch current.Kind {
			case canonical.ItemKindText:
				content = append(content, contentID{
					Type: "text",
					Text: current.Text,
				})
			case canonical.ItemKindToolUse:
				if current.ToolType != "" && current.ToolType != canonical.ToolTypeFunction {
					return nil, canonical.UnsupportedOperation("messages protocol only supports function tool uses")
				}
				input, err := decodeToolArgumentsObject(current.Input)
				if err != nil {
					return nil, err
				}
				toolTypes[current.ToolUseID] = current.ToolType
				content = append(content, contentID{
					Type:  "tool_use",
					ID:    strings.TrimSpace(current.ToolUseID), // swobu:io-string source=boundary
					Name:  strings.TrimSpace(current.Name),      // swobu:io-string source=boundary
					Input: input,
				})
				if strings.TrimSpace(content[len(content)-1].Name) == "" { // swobu:io-string source=boundary
					return nil, canonical.BadRequest("messages protocol tool_use items require a name")
				}
			case canonical.ItemKindToolResult:
				if strings.TrimSpace(current.ToolUseID) == "" { // swobu:io-string source=boundary
					return nil, canonical.BadRequest("messages protocol tool_result items require tool_use_id")
				}
				if toolType := toolTypes[current.ToolUseID]; toolType != "" && toolType != canonical.ToolTypeFunction {
					return nil, canonical.UnsupportedOperation("messages protocol only supports function tool results")
				}
				content = append(content, contentID{
					Type:      "tool_result",
					ToolUseID: strings.TrimSpace(current.ToolUseID), // swobu:io-string source=boundary
					Content:   current.Text,
				})
			default:
				return nil, canonical.UnsupportedOperation("canonical item is not supported on the messages protocol")
			}
			i++
		}
		if len(content) == 0 {
			continue
		}
		out = append(out, messageBody{
			Role:    role,
			Content: content,
		})
	}
	return out, nil
}

func encodeMessagesTools(tools []canonical.ToolDecl, sink effect.Sink, exchangeID string) ([]messagesToolDTO, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	// Messages wire has no strict tool-schema field; provider adapters emit the
	// compatibility decision before this encoder runs.
	out := make([]messagesToolDTO, 0, len(tools))
	for idx, tool := range tools {
		switch decl := tool.(type) {
		case canonical.FunctionToolDecl:
			wire, err := encodeMessagesFunctionToolDecl(decl, sink, exchangeID, idx)
			if err != nil {
				return nil, err
			}
			out = append(out, wire)
		case *canonical.FunctionToolDecl:
			if decl == nil {
				return nil, canonical.BadRequest("messages protocol tool declarations are invalid")
			}
			wire, err := encodeMessagesFunctionToolDecl(*decl, sink, exchangeID, idx)
			if err != nil {
				return nil, err
			}
			out = append(out, wire)
		case canonical.CapabilityToolDecl:
			wire, err := encodeMessagesCapabilityToolDecl(decl, sink, exchangeID, idx)
			if err != nil {
				return nil, err
			}
			out = append(out, wire)
		case *canonical.CapabilityToolDecl:
			if decl == nil {
				return nil, canonical.BadRequest("messages protocol tool declarations are invalid")
			}
			wire, err := encodeMessagesCapabilityToolDecl(*decl, sink, exchangeID, idx)
			if err != nil {
				return nil, err
			}
			out = append(out, wire)
		default:
			return nil, canonical.UnsupportedOperation("messages protocol only supports function and web_search tool declarations")
		}
	}
	return out, nil
}

func encodeMessagesFunctionToolDecl(decl canonical.FunctionToolDecl, sink effect.Sink, exchangeID string, index int) (messagesToolDTO, error) {
	schema, err := messagesToolSchema(decl.ToolInputSchema())
	if err != nil {
		return messagesToolDTO{}, err
	}
	name, err := canonical.ProjectedToolName(decl)
	if err != nil {
		return messagesToolDTO{}, err
	}
	if err := emitMessagesToolNameNamespaceDecision(sink, exchangeID, decl, compat.Approx, compat.Subject("wire:/tools/"+strconv.Itoa(index)+"/name")); err != nil {
		return messagesToolDTO{}, err
	}
	name = strings.TrimSpace(name) // swobu:io-string source=boundary
	if name == "" {
		return messagesToolDTO{}, canonical.BadRequest("messages protocol tool declarations require a name")
	}
	return messagesToolDTO{
		Name:        name,
		Description: strings.TrimSpace(decl.ToolDescription()), // swobu:io-string source=boundary
		InputSchema: schema,
	}, nil
}

func encodeMessagesCapabilityToolDecl(decl canonical.CapabilityToolDecl, sink effect.Sink, exchangeID string, index int) (messagesToolDTO, error) {
	capability := strings.TrimSpace(string(decl.ToolCapability())) // swobu:io-string source=boundary
	switch capability {
	case "web_search":
	default:
		return messagesToolDTO{}, canonical.UnsupportedOperation("messages protocol only supports web_search capability tool declarations")
	}
	if err := emitMessagesToolNameNamespaceDecision(sink, exchangeID, decl, compat.Approx, compat.Subject("wire:/tools/"+strconv.Itoa(index)+"/name")); err != nil {
		return messagesToolDTO{}, err
	}
	return messagesToolDTO{
		Type: "web_search_20250305",
		Name: capability,
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
	return canonical.NewToolSchemaObject(string(normalized)), nil
}

func roleForMessagesItem(item canonical.CanonicalItem) string {
	switch item.Author {
	case canonical.ItemAuthorAssistant:
		return "assistant"
	default:
		return "user"
	}
}

func emitMessagesToolNameNamespaceDecision(sink effect.Sink, exchangeID string, tool canonical.ToolDecl, outcome compat.Outcome, subject compat.Subject) error {
	if sink == nil {
		return nil
	}
	if subject == "" {
		return nil
	}
	if tool != nil && !strings.Contains(strings.TrimSpace(tool.ToolID().Path), "/") { // swobu:io-string source=boundary
		return nil
	}
	if tool == nil && outcome == compat.Approx {
		return nil
	}
	if err := sink.Commit(context.Background(), exchangeID, []effect.Effect{
		effect.CompatibilityEffect{
			Feature: compat.ToolNameNamespace,
			Outcome: outcome,
			Subject: subject,
		},
	}); err != nil {
		return canonical.InternalError("compatibility effect sink commit failed")
	}
	return nil
}

func decodeToolArgumentsObject(input canonical.ToolArguments) (map[string]any, error) {
	raw := input.RawObject()
	trimmedRaw := strings.TrimSpace(raw) // swobu:io-string source=boundary
	if trimmedRaw == "" {
		return map[string]any{}, nil
	}
	out := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, canonical.BadRequest("messages protocol tool_use input must be a JSON object")
	}
	return out, nil
}
