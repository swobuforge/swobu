package exchange

import (
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/report"
	"github.com/swobuforge/swobu/internal/transform"
)

type invariantDocTransform struct {
	id      string
	mutated bool
	losses  []report.Loss
	nextRaw []byte
}

func (t invariantDocTransform) ID() string { return t.id }
func (t invariantDocTransform) Stage() transform.Stage {
	return transform.StageProviderWireOut
}
func (t invariantDocTransform) Match(transform.Context, carrier.WireDocument) bool { return true }
func (t invariantDocTransform) Apply(_ transform.Context, in carrier.WireDocument) (carrier.WireDocument, transform.Report, error) {
	out := in
	out.Raw = append([]byte(nil), t.nextRaw...)
	return out, transform.Report{
		Mutated: t.mutated,
		Losses:  append([]report.Loss(nil), t.losses...),
	}, nil
}

func TestApplyDocumentTransformStage_FailsOnSilentMutation(t *testing.T) {
	reg := transform.NewRegistry([]transform.DocumentTransform{
		invariantDocTransform{
			id:      "silent_change",
			mutated: false,
			nextRaw: []byte(`{"model":"m","input":"changed"}`),
		},
	}, nil)

	_, err := applyDocumentTransformStage(
		reg,
		"ex_invariant",
		transform.StageProviderWireOut,
		carrier.WireDocument{
			Leg:    carrier.LegProviderRequestOut,
			Family: canonical.IngressFamilyResponses,
			Media:  "application/json",
			Raw:    []byte(`{"model":"m","input":"hi"}`),
		},
		delivery.BufferedDelivery(),
	)
	if err == nil {
		t.Fatal("expected invariant error for silent mutation")
	}
}

func TestApplyDocumentTransformStage_FailsOnReportedMutationWithoutChange(t *testing.T) {
	reg := transform.NewRegistry([]transform.DocumentTransform{
		invariantDocTransform{
			id:      "false_mutation",
			mutated: true,
			nextRaw: []byte(`{"model":"m","input":"hi"}`),
		},
	}, nil)

	_, err := applyDocumentTransformStage(
		reg,
		"ex_invariant",
		transform.StageProviderWireOut,
		carrier.WireDocument{
			Leg:    carrier.LegProviderRequestOut,
			Family: canonical.IngressFamilyResponses,
			Media:  "application/json",
			Raw:    []byte(`{"model":"m","input":"hi"}`),
		},
		delivery.BufferedDelivery(),
	)
	if err == nil {
		t.Fatal("expected invariant error for reported mutation without change")
	}
}

func TestApplyDocumentTransformStage_RejectsUnsupportedProjectionLoss(t *testing.T) {
	reg := transform.NewRegistry([]transform.DocumentTransform{
		invariantDocTransform{
			id:      "lossy_change",
			mutated: false,
			nextRaw: []byte(`{"model":"m"}`),
			losses: []report.Loss{{
				Field:    "/input",
				Kind:     report.LossUnsupportedField,
				Reason:   "removed unsupported field",
				Severity: report.SeverityWarning,
			}},
		},
	}, nil)

	_, err := applyDocumentTransformStage(
		reg,
		"ex_invariant",
		transform.StageProviderWireOut,
		carrier.WireDocument{
			Leg:    carrier.LegProviderRequestOut,
			Family: canonical.IngressFamilyResponses,
			Media:  "application/json",
			Raw:    []byte(`{"model":"m","input":"hi"}`),
		},
		delivery.BufferedDelivery(),
	)
	var unsupported UnsupportedProjectionError
	if !errors.As(err, &unsupported) {
		t.Fatalf("expected UnsupportedProjectionError, got %v", err)
	}
}
