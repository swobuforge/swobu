package completions

type completionsRequestDTO struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type completionsResponseDTO struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Model   string                 `json:"model"`
	Choices []completionsChoiceDTO `json:"choices"`
	Usage   *completionsUsageDTO   `json:"usage,omitempty"`
}

type completionsChoiceDTO struct {
	Index        int    `json:"index"`
	Text         string `json:"text"`
	FinishReason string `json:"finish_reason"`
}

type completionsChunkDTO struct {
	Object  string                 `json:"object"`
	Choices []completionsChoiceDTO `json:"choices"`
	Usage   *completionsUsageDTO   `json:"usage,omitempty"`
}

type completionsUsageDTO struct {
	PromptTokens     int                        `json:"prompt_tokens"`
	CompletionTokens int                        `json:"completion_tokens"`
	TotalTokens      int                        `json:"total_tokens"`
	PromptDetails    *completionsPromptUsageDTO `json:"prompt_tokens_details,omitempty"`
}

type completionsPromptUsageDTO struct {
	CachedTokens     int `json:"cached_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
}
