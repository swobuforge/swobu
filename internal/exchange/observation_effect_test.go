package exchange

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
	"github.com/swobuforge/swobu/internal/effect"
	"github.com/swobuforge/swobu/internal/report"
	"github.com/swobuforge/swobu/internal/transform"
)

type recordingEffectSink struct {
	effects []effect.Effect
}

func (s *recordingEffectSink) Commit(_ context.Context, _ string, effects []effect.Effect) error {
	s.effects = append([]effect.Effect(nil), effects...)
	return nil
}

type observationEmittingStreamTransform struct{}

func (observationEmittingStreamTransform) ID() string { return "test.observation" }

func (observationEmittingStreamTransform) Stage() transform.Stage {
	return transform.StageSemanticEvents
}
func (observationEmittingStreamTransform) Capabilities() transform.MiddlewareCapabilities {
	return transform.MiddlewareCapabilities{}
}

func (observationEmittingStreamTransform) Match(transform.Context, canonical.EventReader) bool {
	return true
}

func (observationEmittingStreamTransform) Wrap(_ transform.Context, reader canonical.EventReader) (canonical.EventReader, transform.Outcome, error) {
	return reader, transform.Outcome{
		Observations: []transform.ObservationRecord{{
			Code:   "obs.code",
			Reason: "obs reason",
		}},
		Losses: []report.Loss{{
			Field:      "field",
			Kind:       report.LossUnsupportedField,
			ReasonCode: report.ReasonCode("loss_code"),
			Reason:     "loss reason",
			Severity:   report.SeverityWarning,
		}},
	}, nil
}

func TestRunnerRun_AttachesRouteIdentityToObservationEffects(t *testing.T) {
	sink := &recordingEffectSink{}
	runner := withRuntime(Runner{
		ResolveProviderIngress: bufferedProviderIngressResolver([]byte(`{"id":"resp_1","model":"m","output_text":"ok"}`)),
		Transforms:             transform.NewRegistry(nil, []transform.EventStreamTransform{observationEmittingStreamTransform{}}),
		EffectSink:             sink,
	})

	_, err := runner.Run(context.Background(), ExchangeInput{
		ExchangeID:       "ex_obs",
		ClientFamily:     canonical.ClientFamilyResponses,
		ClientDelivery:   delivery.BufferedDelivery(),
		Request:          testCanonicalRequest("m"),
		Target:           NewRoutableTarget("backend-a", "openai", "https://example.test/v1", "cred-1", protocolkind.Responses, "", "", "responses"),
		Contract:         NewExecutionContract(protocolsurface.BufferedDelivery()),
		ProviderProtocol: protocolkind.Responses,
		ProviderDelivery: delivery.BufferedDelivery(),
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(sink.effects) != 2 {
		t.Fatalf("captured effects len=%d want=2", len(sink.effects))
	}

	var obs effect.ObservationEffect
	var loss effect.LossEffect
	for _, applied := range sink.effects {
		switch e := applied.(type) {
		case effect.ObservationEffect:
			obs = e
		case effect.LossEffect:
			loss = e
		}
	}

	if obs.Observation.RouteID != "backend-a" || obs.Observation.ProviderID != "openai" || obs.Observation.ModelID != "m" {
		t.Fatalf("observation effect route metadata = %#v, want backend-a/openai/m", obs.Observation)
	}
	if obs.Observation.Code != "obs.code" || obs.Observation.Reason != "obs reason" {
		t.Fatalf("observation effect payload = %#v, want obs.code/obs reason", obs.Observation)
	}
	if loss.Observation.RouteID != "backend-a" || loss.Observation.ProviderID != "openai" || loss.Observation.ModelID != "m" {
		t.Fatalf("loss effect route metadata = %#v, want backend-a/openai/m", loss.Observation)
	}
	if loss.Observation.Code != "loss_code" || loss.Observation.Reason != "loss reason" {
		t.Fatalf("loss effect payload = %#v, want loss_code/loss reason", loss.Observation)
	}
}
