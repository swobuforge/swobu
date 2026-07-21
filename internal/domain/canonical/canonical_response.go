package canonical

import (
	"errors"
	"fmt"
	"strings"
)

// CanonicalResponse is the fully materialized canonical success value.
// Construction rejects request-only item states before checkpointing or encoding.
type CanonicalResponse struct {
	response     ResponseRef
	model        string
	items        []CanonicalItem
	finishReason string
	usage        TokenUsage
}

// NewCanonicalResponse constructs the legal model-output subset of canonical
// items. Assistant messages, reasoning, and tool calls are response-producing branches;
// tool results and non-assistant messages are request transcript input.
func NewCanonicalResponse(response ResponseRef, model string, items []CanonicalItem, finishReason string, usage TokenUsage) (CanonicalResponse, error) {
	if response.SwobuID.IsZero() {
		return CanonicalResponse{}, fmt.Errorf("canonical response identity is required")
	}
	if strings.TrimSpace(model) == "" { // swobu:io-string source=domain
		return CanonicalResponse{}, fmt.Errorf("canonical response model is required")
	}
	if strings.TrimSpace(finishReason) == "" { // swobu:io-string source=domain
		return CanonicalResponse{}, fmt.Errorf("canonical response completion reason is required")
	}
	if err := validateResponseItems(items); err != nil {
		return CanonicalResponse{}, err
	}
	return newCanonicalResponse(response, model, items, finishReason, usage), nil
}

func validateResponseItems(items []CanonicalItem) error {
	lifecycle := newResponseToolLifecycleValidator()
	for index, item := range items {
		if err := validateResponseItem(index, item); err != nil {
			return err
		}
		if err := lifecycle.Observe(item); err != nil {
			return fmt.Errorf("canonical response item %d %w", index, err)
		}
	}
	return nil
}

// responseToolLifecycleValidator owns correlation among completed response
// items. Wire streams and materialized responses feed the same transitions.
type responseToolLifecycleValidator struct {
	webSearchCalls map[ToolCallID]struct{}
}

var errWebSearchResultWithoutCall = errors.New("web-search result has no prior call")

func newResponseToolLifecycleValidator() responseToolLifecycleValidator {
	return responseToolLifecycleValidator{webSearchCalls: make(map[ToolCallID]struct{})}
}

func (v *responseToolLifecycleValidator) Observe(item CanonicalItem) error {
	if call, ok := item.ToolCall(); ok && call.Tool().Kind() == ToolKindWebSearch {
		v.webSearchCalls[call.CallID()] = struct{}{}
	}
	if result, ok := item.ToolResult(); ok {
		if _, found := v.webSearchCalls[result.CallID()]; !found {
			return errWebSearchResultWithoutCall
		}
	}
	return nil
}

func validateResponseItem(index int, item CanonicalItem) error {
	switch item.Kind() {
	case ItemKindMessage:
		message, ok := item.Message()
		if !ok || message.Role() != MessageRoleAssistant {
			return fmt.Errorf("canonical response item %d must be an assistant message", index)
		}
	case ItemKindToolCall:
		if _, ok := item.ToolCall(); !ok {
			return fmt.Errorf("canonical response item %d is an invalid tool call", index)
		}
	case ItemKindReasoning:
		if _, ok := item.Reasoning(); !ok || item.Owner() != TurnOwnerAssistant {
			return fmt.Errorf("canonical response item %d is invalid reasoning", index)
		}
	case ItemKindToolResult:
		result, ok := item.ToolResult()
		if !ok {
			return fmt.Errorf("canonical response item %d is an invalid tool result", index)
		}
		if _, search := result.WebSearch(); !search {
			return fmt.Errorf("canonical response item %d is a request-only tool result", index)
		}
	default:
		return fmt.Errorf("canonical response item %d kind %q is unsupported", index, item.Kind())
	}
	return nil
}

func newCanonicalResponse(response ResponseRef, model string, items []CanonicalItem, finishReason string, usage TokenUsage) CanonicalResponse {
	return CanonicalResponse{response: response.Clone(), model: model, items: cloneCanonicalItems(items), finishReason: finishReason, usage: usage}
}

func (o CanonicalResponse) Response() ResponseRef    { return o.response.Clone() }
func (o CanonicalResponse) Model() string            { return o.model }
func (o CanonicalResponse) CompletionReason() string { return o.finishReason }
func (o CanonicalResponse) Items() []CanonicalItem   { return cloneCanonicalItems(o.items) }
func (o CanonicalResponse) Usage() TokenUsage        { return o.usage }

// Clone returns a deep copy suitable for cross-boundary handoff.
func (o CanonicalResponse) Clone() CanonicalResponse {
	return newCanonicalResponse(o.response, o.model, o.items, o.finishReason, o.usage)
}
