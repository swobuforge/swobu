package chatcompletions

import "encoding/json"

type chatCompletionsRequestDTO struct {
	Model                  string                      `json:"model"`
	Messages               []chatCompletionsMessageDTO `json:"messages"`
	PreviousResponseWireID string                      `json:"previous_response_id"`
	Tools                  []ProviderRequestTool       `json:"tools,omitempty"`
	ToolChoice             json.RawMessage             `json:"tool_choice,omitempty"`
	ParallelToolCalls      json.RawMessage             `json:"parallel_tool_calls,omitempty"`
	ResponseFormat         json.RawMessage             `json:"response_format,omitempty"`
	Temperature            json.RawMessage             `json:"temperature,omitempty"`
	MaxTokens              json.RawMessage             `json:"max_tokens,omitempty"`
	MaxCompletionTokens    json.RawMessage             `json:"max_completion_tokens,omitempty"`
	TopP                   json.RawMessage             `json:"top_p,omitempty"`
	Stop                   json.RawMessage             `json:"stop,omitempty"`
	ReasoningEffort        json.RawMessage             `json:"reasoning_effort,omitempty"`
	Stream                 json.RawMessage             `json:"stream,omitempty"`
	StreamOptions          json.RawMessage             `json:"stream_options,omitempty"`
}

type chatCompletionsMessageDTO struct {
	Role       string                       `json:"role"`
	Content    json.RawMessage              `json:"content"`
	ToolCalls  []chatCompletionsToolCallDTO `json:"tool_calls"`
	ToolCallID string                       `json:"tool_call_id"`
}

type chatCompletionsToolCallDTO struct {
	ID       string                            `json:"id"`
	Type     string                            `json:"type"`
	Function *chatCompletionsToolFunctionDTO   `json:"function,omitempty"`
	Custom   *chatCompletionsToolCallCustomDTO `json:"custom,omitempty"`
}

type chatCompletionsToolFunctionDTO struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type chatCompletionsToolCallCustomDTO struct {
	Name  string `json:"name"`
	Input string `json:"input"`
}

type ProviderRequestTool struct {
	Type     string                                    `json:"type"`
	Function *chatCompletionsToolDefinitionFunctionDTO `json:"function,omitempty"`
	Custom   *chatCompletionsToolDefinitionCustomDTO   `json:"custom,omitempty"`
}

type chatCompletionsToolDefinitionFunctionDTO struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
	Strict      *bool           `json:"strict,omitempty"`
}

type chatCompletionsToolDefinitionCustomDTO struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Format      json.RawMessage `json:"format,omitempty"`
}

type chatCompletionsResponseDTO struct {
	ID      string                     `json:"id"`
	Object  string                     `json:"object"`
	Model   string                     `json:"model"`
	Choices []chatCompletionsChoiceDTO `json:"choices"`
	Usage   *chatCompletionsUsageDTO   `json:"usage,omitempty"`
}

type chatCompletionsUsageDTO struct {
	PromptTokens      int                                       `json:"prompt_tokens"`
	CompletionTokens  int                                       `json:"completion_tokens"`
	TotalTokens       int                                       `json:"total_tokens"`
	PromptDetails     *chatCompletionsPromptTokenDetailsDTO     `json:"prompt_tokens_details,omitempty"`
	CompletionDetails *chatCompletionsCompletionTokenDetailsDTO `json:"completion_tokens_details,omitempty"`
}

type chatCompletionsPromptTokenDetailsDTO struct {
	CachedTokens     int `json:"cached_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}

type chatCompletionsCompletionTokenDetailsDTO struct {
	// Zero is meaningful here; omitempty would erase a provider-reported
	// reasoning token count from the protocol surface.
	ReasoningTokens int `json:"reasoning_tokens"`
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

// MarshalJSON makes the actual appendable Chat history value explicit:
// tool-only assistant messages carry content:null, while text and mixed
// messages carry their text string.
func (m chatCompletionsResponseMessageDTO) MarshalJSON() ([]byte, error) {
	type responseMessage struct {
		Role      string                               `json:"role"`
		Content   any                                  `json:"content,omitempty"`
		ToolCalls []chatCompletionsResponseToolCallDTO `json:"tool_calls,omitempty"`
	}
	var content any
	if m.Content != "" {
		content = m.Content
	} else if len(m.ToolCalls) > 0 {
		content = json.RawMessage("null")
	}
	return json.Marshal(responseMessage{Role: m.Role, Content: content, ToolCalls: m.ToolCalls})
}

type chatCompletionsResponseToolCallDTO struct {
	ID       string                              `json:"id"`
	Type     string                              `json:"type"`
	Function *chatCompletionsResponseFunctionDTO `json:"function,omitempty"`
	Custom   *chatCompletionsResponseCustomDTO   `json:"custom,omitempty"`
}

type chatCompletionsResponseFunctionDTO struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatCompletionsResponseCustomDTO struct {
	Name  string `json:"name"`
	Input string `json:"input"`
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
