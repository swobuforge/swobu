package canonical

import (
	"encoding/json"
	"math"
	"strings"
)

// OptionalInt carries one explicitly-set integer without forcing pointer plumbing
// through the canonical model.
type OptionalInt struct {
	value int
	set   bool
}

// NewOptionalInt marks one integer as explicitly set.
func NewOptionalInt(value int) OptionalInt {
	return OptionalInt{value: value, set: true}
}

func (o OptionalInt) IsZero() bool {
	return !o.set
}

func (o OptionalInt) Value() (int, bool) {
	if !o.set {
		return 0, false
	}
	return o.value, true
}

func (o OptionalInt) Clone() OptionalInt {
	return OptionalInt{value: o.value, set: o.set}
}

// OptionalFloat64 carries one explicitly-set float without forcing pointer plumbing
// through the canonical model.
type OptionalFloat64 struct {
	value float64
	set   bool
}

// NewOptionalFloat64 marks one float as explicitly set.
func NewOptionalFloat64(value float64) OptionalFloat64 {
	return OptionalFloat64{value: value, set: true}
}

func (o OptionalFloat64) IsZero() bool {
	return !o.set
}

func (o OptionalFloat64) Value() (float64, bool) {
	if !o.set {
		return 0, false
	}
	return o.value, true
}

func (o OptionalFloat64) Clone() OptionalFloat64 {
	return OptionalFloat64{value: o.value, set: o.set}
}

// GenerationLimits carries the bounded-output and stop-sequence request band.
type GenerationLimits struct {
	MaxOutputTokens OptionalInt
	StopSequences   []string
}

func (l GenerationLimits) Clone() GenerationLimits {
	return GenerationLimits{
		MaxOutputTokens: l.MaxOutputTokens.Clone(),
		StopSequences:   cloneStrings(l.StopSequences),
	}
}

func (l GenerationLimits) IsZero() bool {
	return l.MaxOutputTokens.IsZero() && len(l.StopSequences) == 0
}

// SamplingControls carries provider-neutral sampling knobs for canonical requests.
type SamplingControls struct {
	Temperature OptionalFloat64
	TopP        OptionalFloat64
}

func (s SamplingControls) Clone() SamplingControls {
	return SamplingControls{
		Temperature: s.Temperature.Clone(),
		TopP:        s.TopP.Clone(),
	}
}

func (s SamplingControls) IsZero() bool {
	return s.Temperature.IsZero() && s.TopP.IsZero()
}

// GenerationControls is the canonical request-side generation band.
//
// Fields are kept as a single value object so protocol adapters can lower or
// reject controls explicitly instead of inventing wire-only request state.
type GenerationControls struct {
	Limits   GenerationLimits
	Sampling SamplingControls
}

// GenerationControlsParams is the wire-edge input shape used to construct
// canonical generation controls with validation.
type GenerationControlsParams struct {
	MaxOutputTokens *int
	StopSequences   []string
	Temperature     *float64
	TopP            *float64
}

// NewGenerationControls validates and normalizes one generation-control band.
func NewGenerationControls(params GenerationControlsParams) (GenerationControls, error) {
	controls := GenerationControls{}
	if params.MaxOutputTokens != nil {
		if *params.MaxOutputTokens <= 0 {
			return GenerationControls{}, BadRequest("generation controls max_output_tokens must be greater than zero")
		}
		controls.Limits.MaxOutputTokens = NewOptionalInt(*params.MaxOutputTokens)
	}
	if params.StopSequences != nil {
		if len(params.StopSequences) == 0 {
			return GenerationControls{}, BadRequest("generation controls stop sequence must not be empty")
		}
		stopSequences := make([]string, 0, len(params.StopSequences))
		for _, seq := range params.StopSequences {
			if strings.TrimSpace(seq) == "" { // swobu:io-string source=domain
				return GenerationControls{}, BadRequest("generation controls stop sequence must not be empty")
			}
			stopSequences = append(stopSequences, seq)
		}
		controls.Limits.StopSequences = stopSequences
	}
	if params.Temperature != nil {
		if err := validateGenerationControlTemperature(*params.Temperature); err != nil {
			return GenerationControls{}, err
		}
		controls.Sampling.Temperature = NewOptionalFloat64(*params.Temperature)
	}
	if params.TopP != nil {
		if err := validateGenerationControlTopP(*params.TopP); err != nil {
			return GenerationControls{}, err
		}
		controls.Sampling.TopP = NewOptionalFloat64(*params.TopP)
	}
	return controls, nil
}

func (c GenerationControls) Clone() GenerationControls {
	return GenerationControls{
		Limits:   c.Limits.Clone(),
		Sampling: c.Sampling.Clone(),
	}
}

func (c GenerationControls) IsZero() bool {
	return c.Limits.IsZero() && c.Sampling.IsZero()
}

func validateGenerationControlTemperature(value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return BadRequest("generation controls temperature must be greater than or equal to zero")
	}
	return nil
}

func validateGenerationControlTopP(value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
		return BadRequest("generation controls top_p must be between 0 and 1")
	}
	return nil
}

type generationControlsMetadataDTO struct {
	MaxOutputTokens *int     `json:"max_output_tokens,omitempty"`
	StopSequences   []string `json:"stop_sequences,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"top_p,omitempty"`
}

func decodeGenerationControlsMetadata(raw string) (GenerationControls, error) {
	trimmed := strings.TrimSpace(raw) // swobu:io-string source=domain
	if trimmed == "" || trimmed == "null" {
		return GenerationControls{}, nil
	}
	var dto generationControlsMetadataDTO
	if err := json.Unmarshal([]byte(trimmed), &dto); err != nil {
		return GenerationControls{}, BadRequest("canonical request generation controls are invalid")
	}
	return NewGenerationControls(GenerationControlsParams{
		MaxOutputTokens: dto.MaxOutputTokens,
		StopSequences:   dto.StopSequences,
		Temperature:     dto.Temperature,
		TopP:            dto.TopP,
	})
}

func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	return append([]string(nil), in...)
}
