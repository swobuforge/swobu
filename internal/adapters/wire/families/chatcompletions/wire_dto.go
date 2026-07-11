package chatcompletions

import "encoding/json"

type chatCompletionsRequestDTO struct {
	Model                string                             `json:"model"`
	Messages             []chatCompletionsMessageDTO        `json:"messages"`
	Tools                []chatCompletionsToolDefinitionDTO `json:"tools,omitempty"`
	ToolChoice           json.RawMessage                    `json:"tool_choice,omitempty"`
	MaxTokens            json.RawMessage                    `json:"max_tokens,omitempty"`
	Stream               json.RawMessage                    `json:"stream,omitempty"`
	PromptCacheKey       json.RawMessage                    `json:"prompt_cache_key,omitempty"`
	PromptCacheRetention json.RawMessage                    `json:"prompt_cache_retention,omitempty"`
}

type chatCompletionsMessageDTO struct {
	Role       string                       `json:"role"`
	Content    json.RawMessage              `json:"content"`
	ToolCalls  []chatCompletionsToolCallDTO `json:"tool_calls"`
	ToolCallID string                       `json:"tool_call_id"`
}

type chatCompletionsToolCallDTO struct {
	ID       string                         `json:"id"`
	Type     string                         `json:"type"`
	Function chatCompletionsToolFunctionDTO `json:"function"`
}

type chatCompletionsToolFunctionDTO struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type chatCompletionsToolDefinitionDTO struct {
	Type     string                                   `json:"type"`
	Function chatCompletionsToolDefinitionFunctionDTO `json:"function"`
}

type chatCompletionsToolDefinitionFunctionDTO struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      *bool           `json:"strict,omitempty"`
}

type chatCompletionsResponseDTO struct {
	ID      string                     `json:"id"`
	Object  string                     `json:"object"`
	Model   string                     `json:"model"`
	Choices []chatCompletionsChoiceDTO `json:"choices"`
	Usage   *chatCompletionsUsageDTO   `json:"usage,omitempty"`
}

type chatCompletionsUsageDTO struct {
	PromptTokens     int                                   `json:"prompt_tokens"`
	CompletionTokens int                                   `json:"completion_tokens"`
	TotalTokens      int                                   `json:"total_tokens"`
	PromptDetails    *chatCompletionsPromptTokenDetailsDTO `json:"prompt_tokens_details,omitempty"`
}

type chatCompletionsPromptTokenDetailsDTO struct {
	CachedTokens     int `json:"cached_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}

type chatCompletionsChoiceDTO struct {
	Index        int                               `json:"index"`
	Message      chatCompletionsResponseMessageDTO `json:"message,omitempty"`
	Delta        *chatCompletionsDeltaDTO          `json:"delta,omitempty"`
	FinishReason string                            `json:"finish_reason,omitempty"`
}

type chatCompletionsResponseMessageDTO struct {
	Role      string                               `json:"role"`
	Content   string                               `json:"content,omitempty"`
	ToolCalls []chatCompletionsResponseToolCallDTO `json:"tool_calls,omitempty"`
}

type chatCompletionsResponseToolCallDTO struct {
	ID       string                             `json:"id"`
	Type     string                             `json:"type"`
	Function chatCompletionsResponseFunctionDTO `json:"function"`
}

type chatCompletionsResponseFunctionDTO struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatCompletionsDeltaDTO struct {
	Role      string                            `json:"role,omitempty"`
	Content   string                            `json:"content,omitempty"`
	ToolCalls []chatCompletionsDeltaToolCallDTO `json:"tool_calls,omitempty"`
}

type chatCompletionsDeltaToolCallDTO struct {
	Index    int                             `json:"index"`
	ID       string                          `json:"id,omitempty"`
	Type     string                          `json:"type,omitempty"`
	Function chatCompletionsDeltaFunctionDTO `json:"function"`
}

type chatCompletionsDeltaFunctionDTO struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}
