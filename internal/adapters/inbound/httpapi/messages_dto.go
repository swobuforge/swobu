package httpapi

import "encoding/json"

type messagesRequestDTO struct {
	Model    string               `json:"model"`
	Messages []messagesMessageDTO `json:"messages"`
	Stream   bool                 `json:"stream"`
}

type messagesMessageDTO struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type messagesContentPartDTO struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

type messagesTextPartDTO struct {
	Type string `json:"type"`
	Text string `json:"text"`
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
	Type  string         `json:"type"`
	Text  string         `json:"text,omitempty"`
	ID    string         `json:"id,omitempty"`
	Name  string         `json:"name,omitempty"`
	Input map[string]any `json:"input,omitempty"`
}

type messagesStartEventDTO struct {
	Type    string                  `json:"type"`
	Message messagesStartMessageDTO `json:"message"`
}

type messagesStartMessageDTO struct {
	ID   string `json:"id"`
	Role string `json:"role"`
}

type messagesContentBlockStartDTO struct {
	Type         string                      `json:"type"`
	Index        int                         `json:"index"`
	ContentBlock messagesContentBlockBodyDTO `json:"content_block"`
}

type messagesContentBlockBodyDTO struct {
	Type  string         `json:"type"`
	Text  string         `json:"text,omitempty"`
	ID    string         `json:"id,omitempty"`
	Name  string         `json:"name,omitempty"`
	Input map[string]any `json:"input,omitempty"`
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
}

type messagesContentBlockStopDTO struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}
