package transform

import (
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/report"
)

type severityDocTransform struct {
	id       string
	severity report.Severity
}

func (t severityDocTransform) ID() string                               { return t.id }
func (t severityDocTransform) Stage() Stage                             { return StageProviderWireOut }
func (t severityDocTransform) Match(Context, carrier.WireDocument) bool { return true }
func (t severityDocTransform) Apply(Context, carrier.WireDocument) (carrier.WireDocument, Report, error) {
	return carrier.WireDocument{}, Report{
		Mutated: false,
		Losses: []report.Loss{{
			Field:    "/tools",
			Kind:     report.LossUnrepresentableTool,
			Reason:   "test_loss",
			Severity: t.severity,
		}},
	}, nil
}

func TestApplyDocumentStage_ErrorSeverityBlocksProviderRequest(t *testing.T) {
	reg := NewRegistry([]DocumentTransform{
		severityDocTransform{id: "z", severity: report.SeverityError},
	}, nil)
	_, _, _, _, err := ApplyProviderWireOutStage(carrier.WireDocument{
		Leg:    carrier.LegProviderRequestOut,
		Family: canonical.IngressFamilyResponses,
		Raw:    []byte(`{"model":"m"}`),
	}, reg)
	if err == nil {
		t.Fatal("ApplyProviderWireOutStage() expected error for severity=error loss")
	}
}

func TestApplyDocumentStage_WarningSeverityProducesNotice(t *testing.T) {
	reg := NewRegistry([]DocumentTransform{
		severityDocTransform{id: "z", severity: report.SeverityWarning},
	}, nil)
	_, _, _, notices, err := ApplyProviderWireOutStage(carrier.WireDocument{
		Leg:    carrier.LegProviderRequestOut,
		Family: canonical.IngressFamilyResponses,
		Raw:    []byte(`{"model":"m"}`),
	}, reg)
	if err != nil {
		t.Fatalf("ApplyProviderWireOutStage() error = %v", err)
	}
	if len(notices) != 1 || notices[0].Code != "projection_loss" {
		t.Fatalf("notices=%#v", notices)
	}
}
