package generatecontent

import (
	"bytes"
	"encoding/json"
)

type presentJSON struct {
	Raw     json.RawMessage
	Present bool
}

func (p *presentJSON) UnmarshalJSON(raw []byte) error {
	p.Present = true
	p.Raw = bytes.Clone(raw)
	return nil
}

type requestDTO struct {
	SystemInstruction *contentDTO       `json:"systemInstruction,omitempty"`
	Contents          []contentDTO      `json:"contents"`
	Tools             []toolDTO         `json:"tools,omitempty"`
	ToolConfig        *toolConfigDTO    `json:"toolConfig,omitempty"`
	GenerationConfig  *generationConfig `json:"generationConfig,omitempty"`
	CachedContent     presentJSON       `json:"cachedContent,omitempty"`
	SafetySettings    presentJSON       `json:"safetySettings,omitempty"`
	ServiceTier       presentJSON       `json:"serviceTier,omitempty"`
	Store             presentJSON       `json:"store,omitempty"`
}

type contentDTO struct {
	Role  string    `json:"role,omitempty"`
	Parts []partDTO `json:"parts"`
}

type partDTO struct {
	Text             *string              `json:"text,omitempty"`
	Thought          *bool                `json:"thought,omitempty"`
	ThoughtSignature *string              `json:"thoughtSignature,omitempty"`
	FunctionCall     *functionCallDTO     `json:"functionCall,omitempty"`
	FunctionResponse *functionResponseDTO `json:"functionResponse,omitempty"`
	InlineData       *inlineDataDTO       `json:"inlineData,omitempty"`
	FileData         *json.RawMessage     `json:"fileData,omitempty"`
}

type functionCallDTO struct {
	ID, Name string
	Args     json.RawMessage `json:"args"`
}
type functionResponseDTO struct {
	ID, Name     string
	Response     json.RawMessage   `json:"response"`
	Parts        []responseDataDTO `json:"parts,omitempty"`
	WillContinue *bool             `json:"willContinue,omitempty"`
	Scheduling   *json.RawMessage  `json:"scheduling,omitempty"`
}
type responseDataDTO struct {
	InlineData *inlineDataDTO   `json:"inlineData,omitempty"`
	FileData   *json.RawMessage `json:"fileData,omitempty"`
}
type inlineDataDTO struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}
type toolDTO struct {
	FunctionDeclarations []functionDeclarationDTO `json:"functionDeclarations,omitempty"`
	GoogleSearch         *json.RawMessage         `json:"googleSearch,omitempty"`
}
type functionDeclarationDTO struct {
	Name, Description    string
	Parameters           json.RawMessage `json:"parameters,omitempty"`
	ParametersJSONSchema json.RawMessage `json:"parametersJsonSchema,omitempty"`
	Response             json.RawMessage `json:"response,omitempty"`
	ResponseJSONSchema   json.RawMessage `json:"responseJsonSchema,omitempty"`
}
type toolConfigDTO struct {
	FunctionCallingConfig *functionCallingConfigDTO `json:"functionCallingConfig,omitempty"`
}
type functionCallingConfigDTO struct {
	Mode                 string   `json:"mode,omitempty"`
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}
type generationConfig struct {
	MaxOutputTokens    *int               `json:"maxOutputTokens,omitempty"`
	StopSequences      []string           `json:"stopSequences,omitempty"`
	Temperature        *float64           `json:"temperature,omitempty"`
	TopP               *float64           `json:"topP,omitempty"`
	CandidateCount     *int               `json:"candidateCount,omitempty"`
	TopK               *int               `json:"topK,omitempty"`
	Seed               *int               `json:"seed,omitempty"`
	PresencePenalty    *float64           `json:"presencePenalty,omitempty"`
	FrequencyPenalty   *float64           `json:"frequencyPenalty,omitempty"`
	ResponseLogprobs   *bool              `json:"responseLogprobs,omitempty"`
	Logprobs           *int               `json:"logprobs,omitempty"`
	ResponseMimeType   string             `json:"responseMimeType,omitempty"`
	ResponseSchema     json.RawMessage    `json:"responseSchema,omitempty"`
	ResponseJSONSchema json.RawMessage    `json:"responseJsonSchema,omitempty"`
	ThinkingConfig     *thinkingConfigDTO `json:"thinkingConfig,omitempty"`
}
type thinkingConfigDTO struct {
	ThinkingLevel   string `json:"thinkingLevel,omitempty"`
	ThinkingBudget  *int   `json:"thinkingBudget,omitempty"`
	IncludeThoughts *bool  `json:"includeThoughts,omitempty"`
}
