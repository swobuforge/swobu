package responses

import "encoding/json"

type responsesRequestDTO struct {
	Model                string          `json:"model"`
	Input                json.RawMessage `json:"input"`
	ToolChoice           json.RawMessage `json:"tool_choice"`
	PreviousResponseID   string          `json:"previous_response_id"`
	Conversation         string          `json:"conversation"`
	PromptCacheKey       string          `json:"prompt_cache_key"`
	PromptCacheRetention string          `json:"prompt_cache_retention"`
	Stream               bool            `json:"stream"`
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
	ID         string                   `json:"id"`
	Object     string                   `json:"object"`
	Model      string                   `json:"model"`
	Status     string                   `json:"status"`
	OutputText string                   `json:"output_text"`
	Output     []responsesOutputItemDTO `json:"output"`
	Usage      *responsesUsageDTO       `json:"usage,omitempty"`
}

type responsesUsageDTO struct {
	InputTokens   int                        `json:"input_tokens"`
	OutputTokens  int                        `json:"output_tokens"`
	TotalTokens   int                        `json:"total_tokens"`
	InputDetails  *responsesInputDetailsDTO  `json:"input_tokens_details,omitempty"`
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
}

type responsesCompletedEventDTO struct {
	Type     string                        `json:"type"`
	Response responsesStreamingResponseDTO `json:"response"`
}
