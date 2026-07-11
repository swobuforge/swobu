package exchange

import (
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
)

func TestDeliveryPolicyValidate(t *testing.T) {
	valid := DeliveryPolicy{
		Preferred: delivery.BufferedDelivery(),
		Supported: []delivery.Delivery{delivery.BufferedDelivery(), delivery.StreamingDelivery(delivery.FramingSSE)},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid policy error: %v", err)
	}

	missingPreferred := DeliveryPolicy{
		Preferred: delivery.BufferedDelivery(),
		Supported: []delivery.Delivery{delivery.StreamingDelivery(delivery.FramingSSE)},
	}
	if err := missingPreferred.Validate(); err == nil {
		t.Fatalf("expected missing preferred error")
	}
}

func TestDeliveryPolicyValidate_ExhaustivePreferredMembership(t *testing.T) {
	t.Parallel()

	candidates := []delivery.Delivery{
		delivery.BufferedDelivery(),
		delivery.StreamingDelivery(delivery.FramingSSE),
		delivery.StreamingDelivery(delivery.FramingWebSocket),
		delivery.StreamingDelivery(delivery.FramingNDJSON),
	}

	for _, preferred := range candidates {
		for _, includePreferred := range []bool{false, true} {
			supported := make([]delivery.Delivery, 0, len(candidates))
			for _, d := range candidates {
				if includePreferred || d != preferred {
					supported = append(supported, d)
				}
			}
			policy := DeliveryPolicy{Preferred: preferred, Supported: supported}
			err := policy.Validate()
			if includePreferred && err != nil {
				t.Fatalf("unexpected error includePreferred=true preferred=%v: %v", preferred, err)
			}
			if !includePreferred && err == nil {
				t.Fatalf("expected error includePreferred=false preferred=%v", preferred)
			}
		}
	}
}

func TestRouteSpecValidate(t *testing.T) {
	valid := RouteSpec{
		Client: ClientSurfaceSpec{
			Protocol: ProtocolOpenAIResponses,
			Delivery: DeliveryPolicy{
				Preferred: delivery.BufferedDelivery(),
				Supported: []delivery.Delivery{delivery.BufferedDelivery()},
			},
		},
		Provider: ProviderTargetSpec{
			Protocol: ProtocolOpenAIResponses,
			Delivery: DeliveryPolicy{
				Preferred: delivery.StreamingDelivery(delivery.FramingSSE),
				Supported: []delivery.Delivery{delivery.BufferedDelivery(), delivery.StreamingDelivery(delivery.FramingSSE)},
			},
			Model: ModelProfile{
				Input:     ModelInputSpec{Image: SupportUnknown, File: SupportUnknown},
				Output:    ModelOutputSpec{},
				Tools:     ModelToolSpec{Calls: SupportUnknown, Parallel: SupportUnknown},
				Reasoning: ModelReasoningSpec{Controls: SupportUnknown},
				Limits:    ModelLimitsSpec{ContextTokens: 0, OutputTokens: 0},
			},
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid route spec error: %v", err)
	}

	invalidProtocol := valid
	invalidProtocol.Provider.Protocol = ProtocolID("responses")
	if err := invalidProtocol.Validate(); err == nil {
		t.Fatalf("expected invalid provider protocol id error")
	}

	invalidSupport := valid
	invalidSupport.Provider.Model.Tools.Calls = SupportState("maybe")
	if err := invalidSupport.Validate(); err == nil {
		t.Fatalf("expected invalid support state error")
	}

	invalidLimits := valid
	invalidLimits.Provider.Model.Limits.OutputTokens = -1
	if err := invalidLimits.Validate(); err == nil {
		t.Fatalf("expected invalid limits error")
	}
}
