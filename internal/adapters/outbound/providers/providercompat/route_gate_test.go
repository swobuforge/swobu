package providercompat

import (
	"context"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
)

type recordingEffectSink struct {
	effects []effect.Effect
}

func (s *recordingEffectSink) Commit(_ context.Context, _ string, effects []effect.Effect) error {
	s.effects = append(s.effects, effects...)
	return nil
}

func TestGateRouteFeatureSupport_EmitsSupportedCompatibilityDecisions(t *testing.T) {
	t.Parallel()

	sink := &recordingEffectSink{}
	maxTokens := 64
	temperature := 0.5
	topP := 0.9
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "m",
		Items: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorUser, "hi"),
		},
		Tools: []canonical.ToolDecl{
			canonical.NewFunctionToolDecl("tool_0", "search", "search the workspace", canonical.NewToolSchemaObject(`{"type":"object","properties":{"q":{"type":"string"}}}`)),
		},
		ToolPolicy:    canonical.NewToolPolicy(canonical.ToolPolicyRequired, nil),
		ToolCallBatch: canonical.NewToolCallBatchPolicy(canonical.ToolCallBatchAtMostOne),
		Controls: canonical.GenerationControls{
			Limits: canonical.GenerationLimits{
				MaxOutputTokens: canonical.NewOptionalInt(maxTokens),
				StopSequences:   []string{"END"},
			},
			Sampling: canonical.SamplingControls{
				Temperature: canonical.NewOptionalFloat64(temperature),
				TopP:        canonical.NewOptionalFloat64(topP),
			},
		},
	})

	if err := GateRouteFeatureSupport(context.Background(), sink, "ex-supported", "anthropic", string(protocolkind.Messages), request); err != nil {
		t.Fatalf("anthropic messages support failed: %v", err)
	}
	if len(sink.effects) != 7 {
		t.Fatalf("captured effects len=%d want=7", len(sink.effects))
	}
	want := []compat.Feature{
		compat.GenerationMaxTokens,
		compat.GenerationTemperature,
		compat.GenerationTopP,
		compat.GenerationStopSequences,
		compat.ToolDeclaration,
		compat.RequestParallelTools,
		compat.RequestToolChoice,
	}
	subject := RouteSubject("anthropic", string(protocolkind.Messages))
	for i, effectItem := range sink.effects {
		compatEffect, ok := effectItem.(effect.Compatibility)
		if !ok {
			t.Fatalf("effect[%d] type = %T, want effect.Compatibility", i, effectItem)
		}
		if compatEffect.Feature != want[i] || compatEffect.Outcome != compat.Exact {
			t.Fatalf("effect[%d] = %#v, want %s exact", i, compatEffect, want[i])
		}
		if compatEffect.Subject != subject {
			t.Fatalf("effect[%d] subject = %q, want %q", i, compatEffect.Subject, subject)
		}
	}
}

func TestGateRouteFeatureSupport_RejectsUnsupportedStructuredOutput(t *testing.T) {
	t.Parallel()

	sink := &recordingEffectSink{}
	outputFormat, err := canonical.NewOutputFormat(canonical.OutputFormatParams{
		Kind:   canonical.OutputFormatJSONSchema,
		Name:   "answer",
		Schema: canonical.NewRawJSONObject(`{"type":"object","properties":{"answer":{"type":"string"}}}`),
	})
	if err != nil {
		t.Fatalf("build output format: %v", err)
	}
	request := canonical.NewCanonicalRequest(canonical.RequestParams{
		Model: "m",
		Items: []canonical.CanonicalItem{
			canonical.NewTextItem(canonical.ItemAuthorUser, "hi"),
		},
		OutputFormat: outputFormat,
	})

	gateErr := GateRouteFeatureSupport(context.Background(), sink, "ex-unsupported", "anthropic", string(protocolkind.Messages), request)
	if gateErr == nil || !strings.Contains(gateErr.Error(), "structured JSON schema output") {
		t.Fatalf("unsupported structured output must fail closed, got err=%v", gateErr)
	}
	if len(sink.effects) != 1 {
		t.Fatalf("captured effects len=%d want=1", len(sink.effects))
	}
	compatEffect, ok := sink.effects[0].(effect.Compatibility)
	if !ok {
		t.Fatalf("effect type = %T, want effect.Compatibility", sink.effects[0])
	}
	if compatEffect.Feature != compat.RequestStructuredOutput || compatEffect.Outcome != compat.Reject {
		t.Fatalf("captured effect = %#v, want request.structured_output reject", compatEffect)
	}
	if compatEffect.Subject != RouteSubject("anthropic", string(protocolkind.Messages)) {
		t.Fatalf("captured effect subject = %q, want route subject", compatEffect.Subject)
	}
}
