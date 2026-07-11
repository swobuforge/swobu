package transform

import (
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/report"
)

// ApplyDocumentStage executes document transforms for one stage.
func ApplyDocumentStage(stage Stage, doc carrier.WireDocument, registry Registry) (carrier.WireDocument, []report.Mutation, []report.StageReport, []report.Notice, error) {
	nextDoc, applied, err := registry.ApplyDocument(Context{Stage: stage, Leg: doc.Leg, Carrier: carrier.KindWireDocument, Family: doc.Family}, doc)
	if err != nil {
		return carrier.WireDocument{}, nil, nil, nil, canonical.InternalError("staged request transform failed")
	}
	notices := make([]report.Notice, 0)
	mutations := make([]report.Mutation, 0, len(applied))
	stageApplied := make([]string, 0, len(applied))
	stageMutated := false
	for _, entry := range applied {
		stageApplied = append(stageApplied, entry.ID)
		stageMutated = stageMutated || entry.Mutated
		mutations = append(mutations, report.Mutation{Leg: string(stage), Transform: entry.ID, Changed: entry.Mutated})
		for _, loss := range entry.Losses {
			if err := report.ValidateLoss(loss); err != nil {
				return carrier.WireDocument{}, nil, nil, nil, canonical.InternalError("transform reported invalid projection loss")
			}
			if loss.Severity == report.SeverityError {
				return carrier.WireDocument{}, nil, nil, nil, canonical.BadRequest("transform reported blocking projection loss")
			}
			notices = append(notices, report.Notice{Code: "projection_loss", Field: loss.Field, Reason: loss.Reason})
		}
	}
	var stageReports []report.StageReport
	if len(stageApplied) > 0 {
		stageReports = []report.StageReport{{Stage: string(stage), Carrier: string(carrier.KindWireDocument), Applied: stageApplied, Mutated: stageMutated}}
	}
	return nextDoc, mutations, stageReports, notices, nil
}

// ApplyProviderWireOutStage executes provider-wire-out document transforms.
func ApplyProviderWireOutStage(doc carrier.WireDocument, registry Registry) (carrier.WireDocument, []report.Mutation, []report.StageReport, []report.Notice, error) {
	return ApplyDocumentStage(StageProviderWireOut, doc, registry)
}
