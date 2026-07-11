package strictjson

import (
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/report"
)

// Result reports strict-json document normalization effects.
type Result struct {
	Mutated bool
	Losses  []report.Loss
}

// RemoveUnsupportedFields drops unsupported top-level fields from one provider
// wire document and reports explicit projection loss for removed semantic keys.
func RemoveUnsupportedFields(doc carrier.WireDocument, supported map[string]struct{}) (carrier.WireDocument, Result, error) {
	if len(doc.Raw) == 0 {
		return doc, Result{}, nil
	}
	losses := make([]report.Loss, 0)
	out, mutated, err := carrier.MutateWireDocumentJSON(doc, func(payload map[string]any) (bool, error) {
		changed := false
		for key := range payload {
			if _, ok := supported[key]; ok {
				continue
			}
			delete(payload, key)
			changed = true
			losses = append(losses, report.Loss{
				Field:    key,
				Kind:     report.LossUnsupportedField,
				Reason:   "unsupported_field_removed",
				Severity: report.SeverityWarning,
			})
		}
		return changed, nil
	})
	if err != nil {
		return carrier.WireDocument{}, Result{}, err
	}
	return out, Result{Mutated: mutated, Losses: losses}, nil
}
