package canonical

import (
	"fmt"
	"strings"
)

// CanonicalResponse is the fully materialized canonical terminal value.
// Construction rejects request-only item states before checkpointing or encoding.
type CanonicalResponse struct {
	response   ResponseRef
	model      string
	items      []CanonicalItem
	completion Completion
	usage      TokenUsage
}

// CompletionClass is the closed semantic outcome used by canonical lifecycle
// policy. Provider stop-reason strings remain opaque diagnostics.
type CompletionClass string

const (
	CompletionCompleted  CompletionClass = "completed"
	CompletionIncomplete CompletionClass = "incomplete"
	CompletionDeclined   CompletionClass = "declined"
	CompletionFailed     CompletionClass = "failed"
)

// Completion keeps closed lifecycle policy separate from the original
// provider reason retained for projection and diagnostics.
type Completion struct {
	class  CompletionClass
	reason string
}

func Completed(reason string) Completion {
	return Completion{class: CompletionCompleted, reason: reason}
}

func Incomplete(reason string) Completion {
	return Completion{class: CompletionIncomplete, reason: reason}
}

func Declined(reason string) Completion {
	return Completion{class: CompletionDeclined, reason: reason}
}

func Failed(reason string) Completion {
	return Completion{class: CompletionFailed, reason: reason}
}

func (c Completion) Class() CompletionClass { return c.class }
func (c Completion) Reason() string         { return c.reason }

func (c Completion) validate() error {
	switch c.class {
	case CompletionCompleted, CompletionIncomplete, CompletionDeclined, CompletionFailed:
	default:
		return fmt.Errorf("canonical response completion class is invalid")
	}
	if strings.TrimSpace(c.reason) == "" { // swobu:io-string source=domain
		return fmt.Errorf("canonical response completion reason is required")
	}
	return nil
}

// NewCanonicalResponse constructs the legal model-output subset of canonical
// items. Assistant messages, reasoning, and tool calls are response-producing branches;
// tool results and non-assistant messages are request transcript input.
func NewCanonicalResponse(response ResponseRef, model string, items []CanonicalItem, completion Completion, usage TokenUsage) (CanonicalResponse, error) {
	if response.SwobuID.IsZero() {
		return CanonicalResponse{}, fmt.Errorf("canonical response identity is required")
	}
	if strings.TrimSpace(model) == "" { // swobu:io-string source=domain
		return CanonicalResponse{}, fmt.Errorf("canonical response model is required")
	}
	if err := completion.validate(); err != nil {
		return CanonicalResponse{}, err
	}
	effects, err := validateResponseItems(items)
	if err != nil {
		return CanonicalResponse{}, err
	}
	if completion.Class() == CompletionCompleted {
		if err := effects.RequireSettled(); err != nil {
			return CanonicalResponse{}, err
		}
	}
	return newCanonicalResponse(response, model, items, completion, usage), nil
}

func validateResponseItems(items []CanonicalItem) (responseEffectGuard, error) {
	var effects responseEffectGuard
	for index, item := range items {
		if err := validateResponseItem(index, item); err != nil {
			return responseEffectGuard{}, err
		}
		if err := effects.Accept(index, item); err != nil {
			return responseEffectGuard{}, fmt.Errorf("canonical response item %d %w", index, err)
		}
	}
	return effects, nil
}

// responseEffectGuard applies the response-specific settlement rule after the
// canonical matcher has correlated every completed item. Caller-owned effects
// may remain pending; a successful provider response must settle provider-owned
// search and discovery effects.
type responseEffectGuard struct {
	matcher ToolEffectMatcher
}

func (g *responseEffectGuard) Accept(index int, item CanonicalItem) error {
	_, err := g.matcher.Accept(index, item)
	return err
}

// RequireSettled rejects successful response truth that still owes a
// provider-owned effect. Caller-executed function, custom, and discovery calls
// intentionally remain pending because the response hands them to its caller.
func (g *responseEffectGuard) RequireSettled() error {
	for _, pending := range g.matcher.Pending() {
		switch pending.Kind {
		case ToolKindWebSearch:
			return fmt.Errorf("web-search call has no provider result")
		case ToolKindDiscovery:
			executor, specified := pending.Executor.Get()
			if specified && executor == DiscoveryExecutorProvider {
				return fmt.Errorf("provider-executed tool-discovery call has no provider result")
			}
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
	case ItemKindToolDiscoveryResult:
		result, ok := item.ToolDiscoveryResult()
		if !ok || result.Executor() != DiscoveryExecutorProvider {
			return fmt.Errorf("canonical response item %d is an invalid tool-discovery result", index)
		}
	default:
		return fmt.Errorf("canonical response item %d kind %q is unsupported", index, item.Kind())
	}
	return nil
}

func newCanonicalResponse(response ResponseRef, model string, items []CanonicalItem, completion Completion, usage TokenUsage) CanonicalResponse {
	return CanonicalResponse{response: response.Clone(), model: model, items: cloneCanonicalItems(items), completion: completion, usage: usage}
}

func (o CanonicalResponse) Response() ResponseRef  { return o.response.Clone() }
func (o CanonicalResponse) Model() string          { return o.model }
func (o CanonicalResponse) Completion() Completion { return o.completion }
func (o CanonicalResponse) Items() []CanonicalItem { return cloneCanonicalItems(o.items) }
func (o CanonicalResponse) Usage() TokenUsage      { return o.usage }

// Clone returns a deep copy suitable for cross-boundary handoff.
func (o CanonicalResponse) Clone() CanonicalResponse {
	return newCanonicalResponse(o.response, o.model, o.items, o.completion, o.usage)
}

// WithUsage returns the same semantic response with exchange-aggregated usage.
func (o CanonicalResponse) WithUsage(usage TokenUsage) CanonicalResponse {
	return newCanonicalResponse(o.response, o.model, o.items, o.completion, usage)
}
