package responses

import "encoding/json"

type responsesRequestDTO struct {
	Model              string                       `json:"model"`
	Input              json.RawMessage              `json:"input"`
	ToolChoice         json.RawMessage              `json:"tool_choice"`
	ParallelToolCalls  json.RawMessage              `json:"parallel_tool_calls,omitempty"`
	Tools              []responsesToolDefinitionDTO `json:"tools,omitempty"`
	PreviousResponseID string                       `json:"previous_response_id"`
	Conversation       string                       `json:"conversation"`
	Instructions       json.RawMessage              `json:"instructions,omitempty"`
	Text               *responsesTextDTO            `json:"text,omitempty"`
	Store              json.RawMessage              `json:"store,omitempty"`
	MaxOutputTokens    json.RawMessage              `json:"max_output_tokens,omitempty"`
	Temperature        json.RawMessage              `json:"temperature,omitempty"`
	TopP               json.RawMessage              `json:"top_p,omitempty"`
	Stop               json.RawMessage              `json:"stop,omitempty"`
	Stream             json.RawMessage              `json:"stream,omitempty"`
}

type responsesTextDTO struct {
	Format responsesTextFormatDTO `json:"format,omitempty"`
}

type responsesTextFormatDTO struct {
	Type        string          `json:"type"`
	Name        string          `json:"name,omitempty"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

type responsesWireOutputItemDTO struct {
	Type        string          `json:"type"`
	ID          string          `json:"id,omitempty"`
	Status      string          `json:"status,omitempty"`
	Role        string          `json:"role,omitempty"`
	Content     json.RawMessage `json:"content,omitempty"`
	CallID      string          `json:"call_id,omitempty"`
	Name        string          `json:"name,omitempty"`
	Arguments   string          `json:"arguments,omitempty"`
	Input       string          `json:"input,omitempty"`
	ServerLabel string          `json:"server_label,omitempty"`
}

type responsesInputItemDTO struct {
	Type      string          `json:"type"`
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	CallID    string          `json:"call_id"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Output    json.RawMessage `json:"output"`
}

type responsesOutputTextPartDTO struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesResponseDTO struct {
	ID         string             `json:"id"`
	Object     string             `json:"object"`
	Model      string             `json:"model"`
	Status     string             `json:"status"`
	OutputText string             `json:"output_text"`
	Output     []any              `json:"output"`
	Usage      *responsesUsageDTO `json:"usage,omitempty"`
}

type responsesUsageDTO struct {
	InputTokens   int                        `json:"input_tokens"`
	OutputTokens  int                        `json:"output_tokens"`
	TotalTokens   int                        `json:"total_tokens"`
	InputDetails  *responsesInputDetailsDTO  `json:"input_tokens_details,omitempty"`
	OutputDetails *responsesOutputDetailsDTO `json:"output_tokens_details,omitempty"`
	PromptDetails *responsesPromptDetailsDTO `json:"prompt_tokens_details,omitempty"`
}

type responsesInputDetailsDTO struct {
	// Keep cached_tokens non-omitempty for Responses protocol clients that
	// treat input_tokens_details as a strict object schema once present.
	CachedTokens     int `json:"cached_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}

type responsesPromptDetailsDTO struct {
	CachedTokens     int `json:"cached_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}

type responsesOutputDetailsDTO struct {
	// Zero is meaningful here; omitempty would erase a provider-reported
	// reasoning token count from the protocol surface.
	ReasoningTokens int `json:"reasoning_tokens"`
}

type responsesOutputItemDTO struct {
	Type      string                       `json:"type"`
	Status    string                       `json:"status,omitempty"`
	Role      string                       `json:"role,omitempty"`
	Content   []responsesOutputTextItemDTO `json:"content,omitempty"`
	CallID    string                       `json:"call_id,omitempty"`
	Name      string                       `json:"name,omitempty"`
	Arguments string                       `json:"arguments,omitempty"`
}

type responsesOutputTextItemDTO struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesCreatedEventDTO struct {
	Type     string                        `json:"type"`
	Response responsesStreamingResponseDTO `json:"response"`
}

type responsesStreamingResponseDTO struct {
	ID     string             `json:"id"`
	Object string             `json:"object"`
	Model  string             `json:"model"`
	Status string             `json:"status"`
	Output any                `json:"output,omitempty"`
	Usage  *responsesUsageDTO `json:"usage,omitempty"`
}

type responsesOutputItemEventDTO struct {
	Type        string `json:"type"`
	OutputIndex int    `json:"output_index"`
	Item        any    `json:"item"`
}

type responsesContentPartEventDTO struct {
	Type         string                       `json:"type"`
	ItemID       string                       `json:"item_id"`
	OutputIndex  int                          `json:"output_index"`
	ContentIndex int                          `json:"content_index"`
	Part         responsesOutputTextStreamDTO `json:"part"`
}

type responsesOutputTextStreamDTO struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Annotations []any  `json:"annotations"`
}

type responsesOutputItemMessageDTO struct {
	ID      string                         `json:"id"`
	Type    string                         `json:"type"`
	Status  string                         `json:"status"`
	Role    string                         `json:"role"`
	Content []responsesOutputTextStreamDTO `json:"content"`
}

type responsesOutputItemFunctionCallDTO struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type responsesOutputItemCustomToolCallDTO struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Status string `json:"status"`
	CallID string `json:"call_id"`
	Name   string `json:"name"`
	Input  string `json:"input"`
}

type responsesToolDefinitionDTO struct {
	Type               string                       `json:"type"`
	Name               string                       `json:"name,omitempty"`
	Description        string                       `json:"description,omitempty"`
	Parameters         json.RawMessage              `json:"parameters,omitempty"`
	Strict             *bool                        `json:"strict,omitempty"`
	Format             json.RawMessage              `json:"format,omitempty"`
	Tools              []responsesToolDefinitionDTO `json:"tools,omitempty"`
	Execution          string                       `json:"execution,omitempty"`
	ExternalWebAccess  *bool                        `json:"external_web_access,omitempty"`
	SearchContentTypes []string                     `json:"search_content_types,omitempty"`
	OutputFormat       string                       `json:"output_format,omitempty"`
}

type responsesTextDeltaEventDTO struct {
	Type         string `json:"type"`
	ItemID       string `json:"item_id"`
	OutputIndex  int    `json:"output_index"`
	ContentIndex int    `json:"content_index"`
	Delta        string `json:"delta"`
}

type responsesTextDoneEventDTO struct {
	Type         string `json:"type"`
	ItemID       string `json:"item_id"`
	OutputIndex  int    `json:"output_index"`
	ContentIndex int    `json:"content_index"`
	Text         string `json:"text"`
}

type responsesToolArgumentsDeltaEventDTO struct {
	Type        string `json:"type"`
	ItemID      string `json:"item_id"`
	OutputIndex int    `json:"output_index"`
	CallID      string `json:"call_id"`
	Name        string `json:"name"`
	Delta       string `json:"delta"`
}

type responsesToolArgumentsDoneEventDTO struct {
	Type        string `json:"type"`
	ItemID      string `json:"item_id"`
	OutputIndex int    `json:"output_index"`
	CallID      string `json:"call_id"`
	Name        string `json:"name"`
	// OpenAI-style function_call_arguments.done events carry `arguments`.
	// Custom tool input events use `input`; the encoder sets the matching field.
	Arguments string `json:"arguments,omitempty"`
	Input     string `json:"input,omitempty"`
}

type responsesCompletedEventDTO struct {
	Type     string                        `json:"type"`
	Response responsesStreamingResponseDTO `json:"response"`
}
