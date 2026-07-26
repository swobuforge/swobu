package responses

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/mcp"
	openaiwire "github.com/swobuforge/swobu/internal/wire/openai"
	shared "github.com/swobuforge/swobu/internal/wire/shared"
)

// swobu:lint ignore function-complexity because=responses input decoding keeps all acceptance branches in one protocol boundary helper.
func decodeResponsesInput(raw json.RawMessage, tools []canonical.ToolDeclaration, lite bool, sink compat.Sink, exchangeID string, imageLimits shared.ImageDecodeLimitPolicy, access *mcp.Access) ([]canonical.CanonicalItem, error) {
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
		case "additional_tools":
			if strings.TrimSpace(item.Role) != "developer" { // swobu:io-string source=boundary
				return nil, canonical.NotImplemented("Swobu cannot yet project a Responses additional_tools carrier outside the developer role")
			}
			var wireTools []responsesToolDefinitionDTO
			if err := json.Unmarshal(item.Tools, &wireTools); err != nil {
				return nil, canonical.BadRequest("responses request additional_tools tools are invalid")
			}
			if path, kind, found := findResponsesUnsupportedAdditionalToolKind(wireTools, fmt.Sprintf("/input/%d/tools", idx)); found {
				return nil, canonical.NotImplemented(fmt.Sprintf("Swobu has no canonical projection for Responses request %s tool type %q", path, kind))
			}
			scope := canonical.ContextScopeHistory
			if lite && idx == 0 {
				scope = canonical.ContextScopeRequest
			}
			occurrences, embeddedTools, updatedAccess, err := decodeResponsesToolOccurrences(wireTools, scope, sink, exchangeID, true, *access)
			if err != nil {
				return nil, err
			}
			*access = updatedAccess
			decoded = append(decoded, occurrences...)
			tools, err = mergeResponsesToolDeclarations(tools, embeddedTools)
			if err != nil {
				return nil, err
			}
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
			if lite && idx == 1 && role == "developer" {
				for _, part := range parts {
					message, ok := part.Message()
					if !ok {
						return nil, canonical.BadRequest("Responses Lite base instructions must be a message")
					}
					scoped, err := canonical.NewScopedMessageItem(message.Role(), message.Content(), canonical.ContextScopeRequest)
					if err != nil {
						return nil, err
					}
					decoded = append(decoded, scoped)
				}
			} else {
				decoded = append(decoded, parts...)
			}
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
		case "custom_tool_call":
			callID := strings.TrimSpace(item.CallID) // swobu:io-string source=boundary
			if callID == "" {
				return nil, canonical.BadRequest("responses request custom_tool_call items require call_id")
			}
			if strings.TrimSpace(item.Name) == "" { // swobu:io-string source=boundary
				return nil, canonical.BadRequest("responses request custom_tool_call items require a name")
			}
			rawInput := bytes.TrimSpace(item.Input)
			if len(rawInput) == 0 || bytes.Equal(rawInput, []byte("null")) {
				return nil, canonical.BadRequest("responses request custom_tool_call items require input")
			}
			var input string
			if err := json.Unmarshal(rawInput, &input); err != nil {
				return nil, canonical.BadRequest("responses request custom_tool_call input must be a string")
			}
			toolKey, err := canonical.ToolIdentityFromWire(item.Name, canonical.ToolKindCustom)
			if err != nil {
				return nil, canonical.BadRequest("responses request custom_tool_call has an invalid tool identity")
			}
			canonicalCallID, _ := canonical.NewToolCallID(callID)
			call, err := canonical.NewToolCallItem(canonicalCallID, toolKey, canonical.NewTextToolInput(input))
			if err != nil {
				return nil, canonical.BadRequest("responses request custom_tool_call is invalid")
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
			output, err := decodeResponseOutputParts(item.Output, "function_call_output", imageLimits)
			if err != nil {
				return nil, err
			}
			canonicalCallID, _ := canonical.NewToolCallID(callID)
			result, err := canonical.NewToolResultItem(canonicalCallID, output, false)
			if err != nil {
				return nil, canonical.BadRequest("responses request function_call_output is invalid")
			}
			decoded = append(decoded, result)
		case "custom_tool_call_output":
			callID := strings.TrimSpace(item.CallID) // swobu:io-string source=boundary
			if callID == "" {
				return nil, canonical.BadRequest("responses request custom_tool_call_output items require call_id")
			}
			rawOutput := bytes.TrimSpace(item.Output)
			if len(rawOutput) == 0 || bytes.Equal(rawOutput, []byte("null")) {
				return nil, canonical.BadRequest("responses request custom_tool_call_output items require output")
			}
			output, err := decodeResponseOutputParts(item.Output, "custom_tool_call_output", imageLimits)
			if err != nil {
				return nil, err
			}
			canonicalCallID, _ := canonical.NewToolCallID(callID)
			result, err := canonical.NewToolResultItem(canonicalCallID, output, false)
			if err != nil {
				return nil, canonical.BadRequest("responses request custom_tool_call_output is invalid")
			}
			decoded = append(decoded, result)
		case "tool_search_call":
			executor, ok := decodeResponsesToolExecutor(item.Execution)
			if !ok {
				return nil, canonical.BadRequest("responses tool_search_call execution is invalid")
			}
			callID, err := canonical.NewToolCallID(strings.TrimSpace(item.CallID))
			if err != nil {
				return nil, canonical.BadRequest("responses tool_search_call requires call_id")
			}
			arguments, err := canonical.ParseJSONObject(item.Arguments)
			if err != nil {
				return nil, canonical.BadRequest("responses tool_search_call arguments are invalid")
			}
			call, err := canonical.NewToolDiscoveryCallItem(callID, canonical.NewJSONObjectToolInput(arguments), executor)
			if err != nil {
				return nil, err
			}
			decoded = append(decoded, call)
		case "tool_search_output":
			executor, ok := decodeResponsesToolExecutor(item.Execution)
			if !ok {
				return nil, canonical.BadRequest("responses tool_search_output execution is invalid")
			}
			if strings.TrimSpace(item.Status) != "completed" {
				return nil, canonical.BadRequest("responses tool_search_output must be completed")
			}
			callID, err := canonical.NewToolCallID(strings.TrimSpace(item.CallID))
			if err != nil {
				return nil, canonical.BadRequest("responses tool_search_output requires call_id")
			}
			loaded, err := decodeResponsesAdditionalTools(item.Tools, sink, exchangeID)
			if err != nil {
				return nil, err
			}
			set, err := canonical.NewToolSet(loaded)
			if err != nil {
				return nil, canonical.BadRequest("responses tool_search_output tools are invalid")
			}
			result, err := canonical.NewToolDiscoveryResultItem(callID, set, executor)
			if err != nil {
				return nil, err
			}
			decoded = append(decoded, result)
			tools, err = mergeResponsesToolDeclarations(tools, loaded)
			if err != nil {
				return nil, err
			}
		case "web_search_call":
			rawAction := bytes.TrimSpace(item.Action)
			if len(rawAction) == 0 || bytes.Equal(rawAction, []byte("null")) {
				if strings.TrimSpace(item.Status) != "completed" { // swobu:io-string source=boundary
					return nil, canonical.BadRequest("responses actionless web-search marker must be completed")
				}
				// History fingerprinting already used this known partial item to
				// partition prior output. It has no inference or projection
				// consumer after rebasing, so it does not enter canonical.
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
			state, err := decodeResponsesWebSearchLifecycleState(item.Status)
			if err != nil {
				return nil, canonical.BadRequest("responses request web-search status is unsupported")
			}
			lifecycle, err := decodeResponsesWebSearchLifecycle(callID, rawAction, state)
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
			path := fmt.Sprintf("/input/%d/type", idx)
			return nil, canonical.NotImplemented(fmt.Sprintf("Swobu has no canonical projection for Responses request %s item type %q", path, itemType))
		}
	}
	return decoded, nil
}

func decodeResponsesToolExecutor(raw string) (canonical.DiscoveryExecutor, bool) {
	switch strings.TrimSpace(raw) {
	case "client":
		return canonical.DiscoveryExecutorClient, true
	case "server":
		return canonical.DiscoveryExecutorProvider, true
	default:
		return 0, false
	}
}

func mergeResponsesToolDeclarations(current, added []canonical.ToolDeclaration) ([]canonical.ToolDeclaration, error) {
	merged := append([]canonical.ToolDeclaration(nil), current...)
	for _, candidate := range added {
		found := false
		for _, existing := range merged {
			if existing.Key() != candidate.Key() {
				continue
			}
			if !existing.Equivalent(candidate) {
				return nil, canonical.BadRequest("responses tool environment contains a conflicting declaration")
			}
			found = true
			break
		}
		if !found {
			merged = append(merged, candidate)
		}
	}
	return merged, nil
}

func decodeResponsesAdditionalTools(raw json.RawMessage, sink compat.Sink, exchangeID string) ([]canonical.ToolDeclaration, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || trimmed[0] != '[' {
		return nil, canonical.BadRequest("responses request additional_tools tools must be an array")
	}
	var wireTools []responsesToolDefinitionDTO
	if err := json.Unmarshal(trimmed, &wireTools); err != nil {
		return nil, canonical.BadRequest("responses request additional_tools tools are invalid")
	}
	if path, kind, found := findResponsesUnsupportedAdditionalToolKind(wireTools, "/input/0/tools"); found {
		return nil, canonical.NotImplemented(fmt.Sprintf("Swobu has no canonical projection for Responses request %s tool type %q", path, kind))
	}
	decoded, err := decodeResponsesTools(wireTools, sink, exchangeID)
	if err != nil {
		return nil, err
	}
	if decoded == nil {
		return []canonical.ToolDeclaration{}, nil
	}
	return decoded, nil
}

func findResponsesUnsupportedAdditionalToolKind(tools []responsesToolDefinitionDTO, parentPath string) (string, string, bool) {
	for index, tool := range tools {
		observedKind := strings.TrimSpace(tool.Type)        // swobu:io-string source=boundary
		kind := strings.ToLower(observedKind)               // swobu:io-string source=boundary
		toolPath := fmt.Sprintf("%s/%d", parentPath, index) // swobu:io-string source=boundary
		if kind == "" {
			switch {
			case len(tool.Tools) > 0:
				kind = "namespace"
			case len(tool.Format) > 0:
				kind = "custom"
			default:
				kind = "function"
			}
		}
		switch kind {
		case "namespace":
			if path, nestedKind, found := findResponsesUnsupportedAdditionalToolKind(tool.Tools, toolPath+"/tools"); found {
				return path, nestedKind, true
			}
		case "function", "custom", "tool_search", "web_search", "web_search_preview":
		case "mcp":
			if strings.Count(parentPath, "/tools") != 1 {
				return toolPath + "/type", observedKind, true
			}
		default:
			return toolPath + "/type", observedKind, true
		}
	}
	return "", "", false
}

func equalResponsesToolDeclarations(left, right []canonical.ToolDeclaration) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Kind() != right[index].Kind() || left[index].Key() != right[index].Key() {
			return false
		}
		if leftFunction, ok := left[index].Function(); ok {
			rightFunction, rightOK := right[index].Function()
			if !rightOK ||
				leftFunction.Description() != rightFunction.Description() ||
				leftFunction.InputSchema().RawObject() != rightFunction.InputSchema().RawObject() {
				return false
			}
			leftStrict, leftSpecified := leftFunction.Strict().Get()
			rightStrict, rightSpecified := rightFunction.Strict().Get()
			if leftSpecified != rightSpecified || leftStrict != rightStrict {
				return false
			}
		}
		if leftCustom, ok := left[index].Custom(); ok {
			rightCustom, rightOK := right[index].Custom()
			if !rightOK ||
				leftCustom.Description() != rightCustom.Description() ||
				leftCustom.Format().RawObject() != rightCustom.Format().RawObject() {
				return false
			}
		}
	}
	return true
}
