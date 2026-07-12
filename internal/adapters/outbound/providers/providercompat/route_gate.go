package providercompat

import (
	"context"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/effect"
)

// GateRouteFeatureSupport fails closed before protocol encoding when the
// selected provider route cannot truthfully carry one semantic feature. It
// keeps support decisions at the provider edge rather than inside wire syntax
// adapters.
func GateRouteFeatureSupport(ctx context.Context, sink effect.Sink, exchangeID string, providerID string, protocol string, request canonical.CanonicalRequest) error {
	routeSubject := RouteSubject(providerID, protocol)
	model := request.Model()
	require := func(feature compat.Feature, unsupported error) error {
		support := compat.SupportsFeature(providerID, protocol, model, feature)
		if err := recordRouteFeatureSupportDecision(ctx, sink, exchangeID, routeSubject, feature, support); err != nil {
			return err
		}
		if support != compat.Supported {
			return unsupported
		}
		return nil
	}

	if request.OutputFormat().Kind == canonical.OutputFormatJSONSchema {
		if err := require(compat.RequestStructuredOutput, canonical.UnsupportedOperation("provider target does not support structured JSON schema output")); err != nil {
			return err
		}
	}

	controls := request.Controls()
	if _, ok := controls.Limits.MaxOutputTokens.Value(); ok {
		if err := require(compat.GenerationMaxTokens, canonical.UnsupportedOperation("provider target does not support max output tokens")); err != nil {
			return err
		}
	}
	if _, ok := controls.Sampling.Temperature.Value(); ok {
		if err := require(compat.GenerationTemperature, canonical.UnsupportedOperation("provider target does not support temperature")); err != nil {
			return err
		}
	}
	if _, ok := controls.Sampling.TopP.Value(); ok {
		if err := require(compat.GenerationTopP, canonical.UnsupportedOperation("provider target does not support top_p")); err != nil {
			return err
		}
	}
	if len(controls.Limits.StopSequences) > 0 {
		if err := require(compat.GenerationStopSequences, canonical.UnsupportedOperation("provider target does not support stop sequences")); err != nil {
			return err
		}
	}

	tools := request.Tools()
	if len(tools) > 0 {
		if err := require(compat.ToolDeclaration, canonical.UnsupportedOperation("provider target does not support tool declarations")); err != nil {
			return err
		}
	}

	// Batch lowering is inert without tools; fail closed before encoding when
	// the selected protocol cannot truthfully carry at_most_one.
	if batch := request.ToolCallBatch(); len(tools) > 0 && batch.Mode == canonical.ToolCallBatchAtMostOne {
		if err := require(compat.RequestParallelTools, canonical.UnsupportedOperation("provider target does not support at-most-one tool call batching")); err != nil {
			return err
		}
	}

	// ToolPolicyNone collapses with the zero value, so the gate only checks the
	// positive lowerings that require an explicit protocol surface.
	switch request.ToolPolicy().Mode {
	case canonical.ToolPolicyRequired:
		if err := require(compat.RequestToolChoice, canonical.UnsupportedOperation("provider target does not support required tool choice")); err != nil {
			return err
		}
	case canonical.ToolPolicySpecific:
		if err := require(compat.RequestToolChoice, canonical.UnsupportedOperation("provider target does not support specific tool choice")); err != nil {
			return err
		}
	}

	return nil
}

func recordRouteFeatureSupportDecision(ctx context.Context, sink effect.Sink, exchangeID string, subject compat.Subject, feature compat.Feature, support compat.Support) error {
	if sink == nil {
		return nil
	}
	decision := effect.Compatibility{
		Feature: feature,
		Outcome: compatibilityOutcomeForSupport(support),
		Subject: subject,
	}
	return sink.Commit(ctx, exchangeID, []effect.Effect{decision})
}

func compatibilityOutcomeForSupport(support compat.Support) compat.Outcome {
	switch support {
	case compat.Supported:
		return compat.Exact
	case compat.Partial:
		return compat.Approx
	default:
		return compat.Reject
	}
}
