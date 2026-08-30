package messages

import "encoding/json"

type messagesRequestDTO struct {
	Model                  string                   `json:"model"`
	System                 json.RawMessage          `json:"system,omitempty"`
	Messages               []messagesMessageDTO     `json:"messages"`
	PreviousResponseWireID string                   `json:"previous_response_id"`
	Tools                  []ProviderRequestTool    `json:"tools,omitempty"`
	ToolChoice             json.RawMessage          `json:"tool_choice,omitempty"`
	DisableParallelToolUse json.RawMessage          `json:"disable_parallel_tool_use,omitempty"`
	ResponseFormat         json.RawMessage          `json:"response_format,omitempty"`
	MaxTokens              json.RawMessage          `json:"max_tokens,omitempty"`
	Temperature            json.RawMessage          `json:"temperature,omitempty"`
	TopP                   json.RawMessage          `json:"top_p,omitempty"`
	StopSequences          json.RawMessage          `json:"stop_sequences,omitempty"`
	Stream                 json.RawMessage          `json:"stream,omitempty"`
	Thinking               *messagesThinkingDTO     `json:"thinking,omitempty"`
	OutputConfig           *messagesOutputConfigDTO `json:"output_config,omitempty"`
}

type messagesThinkingDTO struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
	Display      string `json:"display,omitempty"`
}

type messagesOutputConfigDTO struct {
	Effort string                         `json:"effort,omitempty"`
	Format *messagesNativeOutputFormatDTO `json:"format,omitempty"`
}

type messagesNativeOutputFormatDTO struct {
	Type   string          `json:"type"`
	Schema json.RawMessage `json:"schema,omitempty"`
}

type messagesMessageDTO struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// ProviderRequestTool is one typed Messages tool declaration before optional
// exact-provider adaptation and the single JSON serialization boundary.
type ProviderRequestTool struct {
	Type              string          `json:"type,omitempty"`
	Name              string          `json:"name"`
	Description       string          `json:"description,omitempty"`
	InputSchema       json.RawMessage `json:"input_schema,omitempty"`
	MaxUses           *int            `json:"max_uses,omitempty"`
	AllowedDomains    []string        `json:"allowed_domains,omitempty"`
	BlockedDomains    []string        `json:"blocked_domains,omitempty"`
	UserLocation      json.RawMessage `json:"user_location,omitempty"`
	ResponseInclusion string          `json:"response_inclusion,omitempty"`
	AllowedCallers    []string        `json:"allowed_callers,omitempty"`
	DeferLoading      bool            `json:"defer_loading,omitempty"`
}

type messagesResponseDTO struct {
	ID         string                    `json:"id"`
	Type       string                    `json:"type"`
	Role       string                    `json:"role"`
	Model      string                    `json:"model"`
	Content    []messagesResponsePartDTO `json:"content"`
	StopReason string                    `json:"stop_reason"`
	Usage      *messagesUsageDTO         `json:"usage,omitempty"`
}

type messagesUsageDTO struct {
	InputTokens              int                         `json:"input_tokens"`
	OutputTokens             int                         `json:"output_tokens"`
	CacheReadInputTokens     int                         `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int                         `json:"cache_creation_input_tokens,omitempty"`
	ServerToolUse            *messagesServerToolUsageDTO `json:"server_tool_use,omitempty"`
}

type messagesServerToolUsageDTO struct {
	WebSearchRequests int `json:"web_search_requests"`
}

type messagesResponsePartDTO struct {
	Type      string                `json:"type"`
	Text      string                `json:"text,omitempty"`
	ID        string                `json:"id,omitempty"`
	Name      string                `json:"name,omitempty"`
	Input     json.RawMessage       `json:"input,omitempty"`
	Thinking  *string               `json:"thinking,omitempty"`
	Signature string                `json:"signature,omitempty"`
	Data      string                `json:"data,omitempty"`
	ToolUseID string                `json:"tool_use_id,omitempty"`
	Content   json.RawMessage       `json:"content,omitempty"`
	IsError   bool                  `json:"is_error,omitempty"`
	Citations []messagesCitationDTO `json:"citations,omitempty"`
}

type messagesCitationDTO struct {
	Type           string `json:"type"`
	URL            string `json:"url"`
	Title          string `json:"title,omitempty"`
	CitedText      string `json:"cited_text,omitempty"`
	StartCharIndex *int   `json:"start_char_index,omitempty"`
	EndCharIndex   *int   `json:"end_char_index,omitempty"`
}

type messagesStartEventDTO struct {
	Type    string                  `json:"type"`
	Message messagesStartMessageDTO `json:"message"`
}

type messagesStartMessageDTO struct {
	ID           string                    `json:"id"`
	Type         string                    `json:"type"`
	Role         string                    `json:"role"`
	Model        string                    `json:"model"`
	Content      []messagesResponsePartDTO `json:"content"`
	StopReason   *string                   `json:"stop_reason"`
	StopSequence *string                   `json:"stop_sequence"`
	Usage        messagesUsageDTO          `json:"usage"`
}

type messagesDeltaEventDTO struct {
	Type  string                `json:"type"`
	Delta messagesDeltaBodyDTO  `json:"delta"`
	Usage messagesDeltaUsageDTO `json:"usage"`
}

type messagesDeltaBodyDTO struct {
	StopReason   string  `json:"stop_reason"`
	StopSequence *string `json:"stop_sequence"`
}

type messagesDeltaUsageDTO struct {
	InputTokens              int                         `json:"input_tokens,omitempty"`
	OutputTokens             int                         `json:"output_tokens"`
	CacheReadInputTokens     int                         `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int                         `json:"cache_creation_input_tokens,omitempty"`
	ServerToolUse            *messagesServerToolUsageDTO `json:"server_tool_use,omitempty"`
}

type messagesContentBlockStartDTO struct {
	Type         string                      `json:"type"`
	Index        int                         `json:"index"`
	ContentBlock messagesContentBlockBodyDTO `json:"content_block"`
}

type messagesContentBlockBodyDTO struct {
	Type      string                `json:"type"`
	Text      string                `json:"text,omitempty"`
	ID        string                `json:"id,omitempty"`
	Name      string                `json:"name,omitempty"`
	Input     json.RawMessage       `json:"input,omitempty"`
	Thinking  *string               `json:"thinking,omitempty"`
	Signature string                `json:"signature,omitempty"`
	Data      string                `json:"data,omitempty"`
	ToolUseID string                `json:"tool_use_id,omitempty"`
	Content   json.RawMessage       `json:"content,omitempty"`
	IsError   bool                  `json:"is_error,omitempty"`
	Citations []messagesCitationDTO `json:"citations,omitempty"`
}

type messagesContentBlockDeltaDTO struct {
	Type  string                           `json:"type"`
	Index int                              `json:"index"`
	Delta messagesContentBlockDeltaBodyDTO `json:"delta"`
}

type messagesContentBlockDeltaBodyDTO struct {
	Type        string               `json:"type"`
	Text        string               `json:"text,omitempty"`
	PartialJSON string               `json:"partial_json,omitempty"`
	Thinking    string               `json:"thinking,omitempty"`
	Signature   string               `json:"signature,omitempty"`
	Citation    *messagesCitationDTO `json:"citation,omitempty"`
}

type messagesContentBlockStopDTO struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}
