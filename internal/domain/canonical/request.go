package canonical

import "strings"

type SemanticKind string

const (
	SemanticKindConversation SemanticKind = "conversation"
	SemanticKindResponse     SemanticKind = "response_generation"
	SemanticKindPrompt       SemanticKind = "prompt_generation"
)

type ToolMode string

const (
	ToolModeDefault  ToolMode = ""
	ToolModeAuto     ToolMode = "auto"
	ToolModeRequired ToolMode = "required"
)

type CanonicalRequest interface {
	// SemanticKind reports which semantic family this canonical request represents.
	SemanticKind() SemanticKind
	// Clone returns a deep semantic copy suitable for safe cross-boundary handoff.
	Clone() CanonicalRequest
}

type DialogCanonicalRequest struct {
	model string
	items []CanonicalItem
}

// NewDialogRequest builds the canonical request used for conversation-like
// semantics across chat and messages-style ingress families.
func NewDialogRequest(model string, items []CanonicalItem) DialogCanonicalRequest {
	return DialogCanonicalRequest{
		model: model,
		items: cloneCanonicalItems(items),
	}
}

func (r DialogCanonicalRequest) SemanticKind() SemanticKind {
	return SemanticKindConversation
}

func (r DialogCanonicalRequest) Model() string {
	return r.model
}

func (r DialogCanonicalRequest) Items() []CanonicalItem {
	return cloneCanonicalItems(r.items)
}

func (r DialogCanonicalRequest) Clone() CanonicalRequest {
	return NewDialogRequest(r.model, r.items)
}

type GenerationCanonicalRequest struct {
	model string
	// thread is the authoritative semantic history including the current turn.
	thread []CanonicalItem
	// lastTurn is an optional derived suffix used when a target protocol can
	// exploit continuation truthfully, including native responses chaining and
	// canonical * -> responses realization when prior thread state is available.
	lastTurn                []CanonicalItem
	previousResponseID      string
	unsupportedConversation string
	invalidContinuationPair bool
	toolMode                ToolMode
	promptCacheKey          string
	promptCacheRetention    string
}

// GenerationRequestParams accepts both authored wire-like input and already
// prepared canonical continuity views. Constructors normalize that into one
// semantic request so adapters do not need to guess which fields are
// authoritative.
type GenerationRequestParams struct {
	Model                string
	InputText            string
	Items                []CanonicalItem
	Thread               []CanonicalItem
	LastTurn             []CanonicalItem
	PreviousResponseID   string
	ConversationID       string
	ToolMode             ToolMode
	PromptCacheKey       string
	PromptCacheRetention string
}

// NewGenerationRequest builds the canonical request for response-generation semantics.
// Continuity and cache-related fields stay here so provider adapters can preserve
// cost- and behavior-sensitive response API features explicitly.
func NewGenerationRequest(params GenerationRequestParams) GenerationCanonicalRequest {
	previousResponseID := strings.TrimSpace(params.PreviousResponseID) // swobu:io-string source=domain
	conversationID := strings.TrimSpace(params.ConversationID)         // swobu:io-string source=domain
	invalidContinuationPair := previousResponseID != "" && conversationID != ""

	authoredThread := cloneCanonicalItems(params.Items)
	if params.InputText != "" {
		authoredThread = append(authoredThread, NewTextItem(ItemAuthorUser, params.InputText))
	}
	thread := cloneCanonicalItems(params.Thread)
	if thread == nil {
		thread = authoredThread
	}
	lastTurn := cloneCanonicalItems(params.LastTurn)
	if lastTurn == nil {
		lastTurn = authoredThread
	}
	return GenerationCanonicalRequest{
		model:                   params.Model,
		thread:                  thread,
		lastTurn:                lastTurn,
		previousResponseID:      previousResponseID,
		unsupportedConversation: conversationID,
		invalidContinuationPair: invalidContinuationPair,
		toolMode:                params.ToolMode,
		promptCacheKey:          params.PromptCacheKey,
		promptCacheRetention:    params.PromptCacheRetention,
	}
}

func (r GenerationCanonicalRequest) SemanticKind() SemanticKind {
	return SemanticKindResponse
}

func (r GenerationCanonicalRequest) Model() string {
	return r.model
}

func (r GenerationCanonicalRequest) Thread() []CanonicalItem {
	return cloneCanonicalItems(r.thread)
}

func (r GenerationCanonicalRequest) LastTurn() []CanonicalItem {
	return cloneCanonicalItems(r.lastTurn)
}

func (r GenerationCanonicalRequest) PreviousResponseID() string {
	return r.previousResponseID
}

func (r GenerationCanonicalRequest) ConversationID() string {
	return r.unsupportedConversation
}

func (r GenerationCanonicalRequest) ToolMode() ToolMode {
	return r.toolMode
}

func (r GenerationCanonicalRequest) PromptCacheKey() string {
	return r.promptCacheKey
}

func (r GenerationCanonicalRequest) PromptCacheRetention() string {
	return r.promptCacheRetention
}

func (r GenerationCanonicalRequest) HasThread() bool {
	return len(r.thread) > 0
}

func (r GenerationCanonicalRequest) HasLastTurn() bool {
	return len(r.lastTurn) > 0
}

func (r GenerationCanonicalRequest) Clone() CanonicalRequest {
	return NewGenerationRequest(GenerationRequestParams{
		Model:                r.model,
		Thread:               r.thread,
		LastTurn:             r.lastTurn,
		PreviousResponseID:   r.previousResponseID,
		ConversationID:       r.unsupportedConversation,
		ToolMode:             r.toolMode,
		PromptCacheKey:       r.promptCacheKey,
		PromptCacheRetention: r.promptCacheRetention,
	})
}

type PromptCanonicalRequest struct {
	model  string
	prompt string
}

// NewPromptRequest builds the canonical request for plain prompt-generation semantics.
func NewPromptRequest(model string, prompt string) PromptCanonicalRequest {
	return PromptCanonicalRequest{
		model:  model,
		prompt: prompt,
	}
}

func (r PromptCanonicalRequest) SemanticKind() SemanticKind {
	return SemanticKindPrompt
}

func (r PromptCanonicalRequest) Model() string {
	return r.model
}

func (r PromptCanonicalRequest) Prompt() string {
	return r.prompt
}

func (r PromptCanonicalRequest) Clone() CanonicalRequest {
	return NewPromptRequest(r.model, r.prompt)
}

// CloneCanonicalRequest protects the provider and app seams from accidental mutation
// of canonical inputs after a request has been accepted.
func CloneCanonicalRequest(req CanonicalRequest) CanonicalRequest {
	if req == nil {
		return nil
	}
	return req.Clone()
}

func cloneStringAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = cloneAny(value)
	}
	return out
}

func cloneAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneStringAnyMap(typed)
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = cloneAny(typed[i])
		}
		return out
	default:
		return typed
	}
}
