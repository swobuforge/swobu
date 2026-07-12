package canonical

import "encoding/json"

func encodeGenerationControlsMetadata(controls GenerationControls) (string, error) {
	if controls.IsZero() {
		return "", nil
	}
	dto := generationControlsMetadataDTO{}
	if value, ok := controls.Limits.MaxOutputTokens.Value(); ok {
		dto.MaxOutputTokens = &value
	}
	if len(controls.Limits.StopSequences) > 0 {
		dto.StopSequences = cloneStrings(controls.Limits.StopSequences)
	}
	if value, ok := controls.Sampling.Temperature.Value(); ok {
		dto.Temperature = &value
	}
	if value, ok := controls.Sampling.TopP.Value(); ok {
		dto.TopP = &value
	}
	raw, err := json.Marshal(dto)
	if err != nil {
		return "", InternalError("canonical request generation controls could not be encoded")
	}
	return string(raw), nil
}

func encodeToolCallBatchMetadata(policy ToolCallBatchPolicy) (string, error) {
	if policy.IsZero() {
		return "", nil
	}
	if err := policy.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(requestToolCallBatchMetadataDTO{Mode: string(policy.Mode)})
	if err != nil {
		return "", InternalError("canonical request tool call batch policy could not be encoded")
	}
	return string(raw), nil
}

func encodeOutputFormatMetadata(format OutputFormat) (string, error) {
	if format.IsZero() {
		return "", nil
	}
	type outputFormatMetadataDTO struct {
		Kind        string          `json:"kind"`
		Name        string          `json:"name,omitempty"`
		Description string          `json:"description,omitempty"`
		Schema      json.RawMessage `json:"schema,omitempty"`
		Strict      *bool           `json:"strict,omitempty"`
	}
	dto := outputFormatMetadataDTO{
		Kind:        string(format.Kind),
		Name:        format.Name,
		Description: format.Description,
	}
	if !format.Schema.IsEmpty() {
		dto.Schema = json.RawMessage(format.Schema.RawObject())
	}
	if format.Strict {
		strict := true
		dto.Strict = &strict
	}
	raw, err := json.Marshal(dto)
	if err != nil {
		return "", InternalError("canonical request output format could not be encoded")
	}
	return string(raw), nil
}
