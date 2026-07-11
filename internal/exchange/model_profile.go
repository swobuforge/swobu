package exchange

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/delivery"
)

// RouteSpec describes client and provider exchange surfaces.
type RouteSpec struct {
	Client   ClientSurfaceSpec
	Provider ProviderTargetSpec
}

type ClientSurfaceSpec struct {
	Protocol ProtocolID
	Delivery DeliveryPolicy
}

type ProviderTargetSpec struct {
	Protocol ProtocolID
	Endpoint EndpointPolicy
	Model    ModelProfile
	Delivery DeliveryPolicy
}

type ProtocolID string

const (
	ProtocolOpenAIChatCompletions ProtocolID = "openai.chat_completions"
	ProtocolOpenAIResponses       ProtocolID = "openai.responses"
	ProtocolOpenAICompletions     ProtocolID = "openai.completions"
	ProtocolAnthropicMessages     ProtocolID = "anthropic.messages"
)

type DeliveryPolicy struct {
	Preferred delivery.Delivery
	Supported []delivery.Delivery
}

type EndpointPolicy struct {
	JSON       JSONPolicy
	Tools      ToolWirePolicy
	Content    ContentWirePolicy
	Stream     StreamPolicy
	Continuity ContinuityPolicy
}

type JSONPolicy struct {
	RejectUnknownFields bool
}

type ToolWirePolicy struct {
	SupportsHostedTools   bool
	SupportsFunctionTools bool
}

type ContentWirePolicy struct {
	SupportsFileParts bool
}

type StreamPolicy struct {
	SupportsSSE       bool
	SupportsWebSocket bool
	SupportsNDJSON    bool
}

type ContinuityPolicy struct {
	RequiresOpaqueState bool
}

type SupportState string

const (
	SupportYes     SupportState = "yes"
	SupportNo      SupportState = "no"
	SupportUnknown SupportState = "unknown"
)

type ModelProfile struct {
	Input     ModelInputSpec
	Output    ModelOutputSpec
	Tools     ModelToolSpec
	Reasoning ModelReasoningSpec
	Limits    ModelLimitsSpec
}

func unknownModelProfile() ModelProfile {
	return ModelProfile{
		Input:     ModelInputSpec{Image: SupportUnknown, File: SupportUnknown},
		Output:    ModelOutputSpec{},
		Tools:     ModelToolSpec{Calls: SupportUnknown, Parallel: SupportUnknown},
		Reasoning: ModelReasoningSpec{Controls: SupportUnknown},
		Limits:    ModelLimitsSpec{ContextTokens: 0, OutputTokens: 0},
	}
}

type ModelInputSpec struct {
	Image SupportState
	File  SupportState
}

type ModelOutputSpec struct{}

type ModelToolSpec struct {
	Calls    SupportState
	Parallel SupportState
}

type ModelReasoningSpec struct {
	Controls SupportState
}

type ModelLimitsSpec struct {
	ContextTokens int
	OutputTokens  int
}

func (s RouteSpec) Validate() error {
	if err := s.Client.Protocol.Validate(); err != nil {
		return fmt.Errorf("client protocol is invalid")
	}
	if err := s.Provider.Protocol.Validate(); err != nil {
		return fmt.Errorf("provider protocol is invalid")
	}
	if err := s.Client.Delivery.Validate(); err != nil {
		return fmt.Errorf("client delivery policy is invalid")
	}
	if err := s.Provider.Delivery.Validate(); err != nil {
		return fmt.Errorf("provider delivery policy is invalid")
	}
	if err := s.Provider.Model.Validate(); err != nil {
		return fmt.Errorf("model profile is invalid")
	}
	return nil
}

func (p ProtocolID) Validate() error {
	switch p {
	case ProtocolOpenAIChatCompletions, ProtocolOpenAIResponses, ProtocolOpenAICompletions, ProtocolAnthropicMessages:
		return nil
	default:
		return fmt.Errorf("protocol id is invalid")
	}
}

func (p DeliveryPolicy) Validate() error {
	if err := p.Preferred.Validate(); err != nil {
		return fmt.Errorf("preferred delivery is invalid")
	}
	if len(p.Supported) == 0 {
		return fmt.Errorf("supported deliveries are required")
	}
	hasPreferred := false
	for _, supported := range p.Supported {
		if err := supported.Validate(); err != nil {
			return fmt.Errorf("supported delivery is invalid")
		}
		if supported == p.Preferred {
			hasPreferred = true
		}
	}
	if !hasPreferred {
		return fmt.Errorf("preferred delivery must be listed in supported deliveries")
	}
	return nil
}

func (s SupportState) Validate() error {
	switch s {
	case SupportYes, SupportNo, SupportUnknown:
		return nil
	default:
		return fmt.Errorf("support state is invalid")
	}
}

func (m ModelProfile) Validate() error {
	if err := m.Input.Image.Validate(); err != nil {
		return err
	}
	if err := m.Input.File.Validate(); err != nil {
		return err
	}
	if err := m.Tools.Calls.Validate(); err != nil {
		return err
	}
	if err := m.Tools.Parallel.Validate(); err != nil {
		return err
	}
	if err := m.Reasoning.Controls.Validate(); err != nil {
		return err
	}
	if m.Limits.ContextTokens < 0 || m.Limits.OutputTokens < 0 {
		return fmt.Errorf("model limits must be non-negative")
	}
	return nil
}
