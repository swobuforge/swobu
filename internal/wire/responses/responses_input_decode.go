package responses

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

// swobu:lint ignore function-complexity because=responses input decoding keeps all acceptance branches in one protocol boundary helper.
func decodeResponsesInput(raw json.RawMessage, tools []canonical.ToolDeclaration, sink compat.Sink, exchangeID string, imageLimits shared.ImageDecodeLimitPolicy) ([]canonical.CanonicalItem, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw))) // swobu:io-string source=boundary
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		message, err := canonical.NewMessageItem(canonical.MessageRoleUser, []canonical.MessagePart{canonical.NewTextMessagePart(text)})
		if err != nil {
			return nil, canonical.BadRequest("responses request input text is invalid")
		}
		return []canonical.CanonicalItem{message}, nil
	}
	var items []responsesInputItemDTO
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, canonical.BadRequest("responses request input is invalid")
	}
	decoded := make([]canonical.CanonicalItem, 0, len(items))
	for idx, item := range items {
		itemType := strings.TrimSpace(item.Type) // swobu:io-string source=boundary
		if itemType == "" {
			if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestItemsKind, compat.Approx, responsesInputSubject(idx, "type")); err != nil {
				return nil, err
			}
			itemType = "message"
		}
		switch itemType {
		case "message":
			role := strings.TrimSpace(item.Role) // swobu:io-string source=boundary
			if role == "" {
				if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestItemsMessageRole, compat.Approx, responsesInputSubject(idx, "role")); err != nil {
					return nil, err
				}
				role = "user"
			}
			author := openaiwire.AuthorForRole(role)
			if role == "system" {
				author = canonical.MessageRoleSystem
			} else if role == "developer" {
				author = canonical.MessageRoleDeveloper
			}
			parts, err := decodeResponsesMessageContent(item.Content, author, imageLimits)
			if err != nil {
				return nil, err
			}
			decoded = append(decoded, parts...)
		case "function_call":
			callID := strings.TrimSpace(item.CallID) // swobu:io-string source=boundary
			if callID == "" {
				callID = strings.TrimSpace(item.ID) // swobu:io-string source=boundary
			}
			if callID == "" {
				if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestItemsToolCallCallID, compat.Approx, responsesInputSubject(idx, "call_id")); err != nil {
					return nil, err
				}
				callID = openaiwire.GeneratedToolUseID(idx, 0)
			} else if strings.TrimSpace(item.CallID) == "" { // swobu:io-string source=boundary
				if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestItemsToolCallCallID, compat.Approx, responsesInputSubject(idx, "call_id")); err != nil {
					return nil, err
				}
			}
			if strings.TrimSpace(item.Name) == "" { // swobu:io-string source=boundary
				return nil, canonical.BadRequest("responses request function_call items require a name")
			}
			input, err := decodeResponsesFunctionCallArguments(item.Arguments)
			if err != nil {
				return nil, err
			}
			toolKey, err := canonical.ResolveHistoricalToolKeyByName(tools, item.Name, canonical.ToolKindFunction)
			if err != nil {
				return nil, canonical.BadRequest("responses request function_call has an invalid tool identity")
			}
			canonicalCallID, _ := canonical.NewToolCallID(callID)
			call, err := canonical.NewToolCallItem(canonicalCallID, toolKey, canonical.NewJSONObjectToolInput(input))
			if err != nil {
				return nil, canonical.BadRequest("responses request function_call is invalid")
			}
			decoded = append(decoded, call)
		case "function_call_output":
			callID := strings.TrimSpace(item.CallID) // swobu:io-string source=boundary
			if callID == "" {
				if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestItemsToolResultCallID, compat.Reject, responsesInputSubject(idx, "call_id")); err != nil {
					return nil, err
				}
				return nil, canonical.BadRequest("responses request function_call_output items require call_id")
			}
			output, err := decodeResponseOutputParts(item.Output, imageLimits)
			if err != nil {
				return nil, err
			}
			canonicalCallID, _ := canonical.NewToolCallID(callID)
			result, err := canonical.NewToolResultItem(canonicalCallID, output, false)
			if err != nil {
				return nil, canonical.BadRequest("responses request function_call_output is invalid")
			}
			decoded = append(decoded, result)
		case "web_search_call":
			rawAction := bytes.TrimSpace(item.Action)
			if len(rawAction) == 0 || bytes.Equal(rawAction, []byte("null")) {
				// Some Codex durable rollouts retain only the completed native
				// marker. responsesnative.Items already preserves that exact object;
				// portable canonical must not fabricate an unobserved search action.
				continue
			}
			callID := strings.TrimSpace(item.ID) // swobu:io-string source=boundary
			if callID == "" {
				// Codex durable rollouts omit provider presentation IDs when they
				// replay completed search items after a client or daemon restart.
				// Canonical pairing still needs a request-local stable identity.
				if err := emitResponsesCompatibilityDecision(sink, exchangeID, compat.RequestItemsToolCallCallID, compat.Approx, responsesInputSubject(idx, "id")); err != nil {
					return nil, err
				}
				callID = openaiwire.GeneratedToolUseID(idx, 0)
			}
			lifecycle, err := decodeResponsesWebSearchLifecycle(callID, rawAction, strings.TrimSpace(item.Status) == "completed") // swobu:io-string source=boundary
			if err != nil {
				return nil, canonical.BadRequest("responses request web-search history is invalid")
			}
			decoded = append(decoded, lifecycle...)
		case "reasoning":
			reasoning, present, err := decodeResponsesReasoningItem(responsesWireOutputItemDTO{
				Type: item.Type, ID: item.ID, Status: item.Status, Summary: item.Summary, Content: item.Content, EncryptedContent: item.EncryptedContent,
			})
			if err != nil {
				return nil, canonical.BadRequest("responses request reasoning item is invalid")
			}
			if present {
				decoded = append(decoded, reasoning)
			}
		default:
			// The Responses-native input sequence preserves this complete item.
			// Portable canonical assigns no speculative meaning to unknown kinds.
		}
	}
	return decoded, nil
}
