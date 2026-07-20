package messages

import "encoding/json"

type messagesRequestDTO struct {
	Model                  string                   `json:"model"`
	System                 json.RawMessage          `json:"system,omitempty"`
	Messages               []messagesMessageDTO     `json:"messages"`
	PreviousResponseWireID string                   `json:"previous_response_id"`
	Tools                  []messagesToolDTO        `json:"tools,omitempty"`
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
	Effort string `json:"effort,omitempty"`
}

type messagesMessageDTO struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type messagesToolDTO struct {
	Type              string          `json:"type,omitempty"`
	Name              string          `json:"name"`
	Description       string          `json:"description,omitempty"`
	InputSchema       json.RawMessage `json:"input_schema,omitempty"`
	MaxUses           *int            `json:"max_uses,omitempty"`
	AllowedDomains    []string        `json:"allowed_domains,omitempty"`
	BlockedDomains    []string        `json:"blocked_domains,omitempty"`
	UserLocation      json.RawMessage `json:"user_location,omitempty"`
	ResponseInclusion string          `json:"response_inclusion,omitempty"`
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
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
}

type messagesResponsePartDTO struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Thinking  *string         `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	Data      string          `json:"data,omitempty"`
}

type messagesStartEventDTO struct {
	Type    string                  `json:"type"`
	Message messagesStartMessageDTO `json:"message"`
	Usage   messagesUsageDTO        `json:"usage"`
}

type messagesStartMessageDTO struct {
	ID           string                    `json:"id"`
	Type         string                    `json:"type"`
	Role         string                    `json:"role"`
	Content      []messagesResponsePartDTO `json:"content"`
	StopReason   *string                   `json:"stop_reason"`
	StopSequence *string                   `json:"stop_sequence"`
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
	OutputTokens int `json:"output_tokens"`
}

type messagesContentBlockStartDTO struct {
	Type         string                      `json:"type"`
	Index        int                         `json:"index"`
	ContentBlock messagesContentBlockBodyDTO `json:"content_block"`
}

type messagesContentBlockBodyDTO struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Thinking  *string         `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	Data      string          `json:"data,omitempty"`
}

type messagesContentBlockDeltaDTO struct {
	Type  string                           `json:"type"`
	Index int                              `json:"index"`
	Delta messagesContentBlockDeltaBodyDTO `json:"delta"`
}

type messagesContentBlockDeltaBodyDTO struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	Signature   string `json:"signature,omitempty"`
}

type messagesContentBlockStopDTO struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}
