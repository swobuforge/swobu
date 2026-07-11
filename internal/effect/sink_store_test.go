package effect

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/observation"
	"github.com/swobuforge/swobu/internal/report"
)

type recordingObservationStore struct {
	records []observation.ObservationRecord
}

func (s *recordingObservationStore) Put(_ context.Context, obs observation.ObservationRecord) error {
	s.records = append(s.records, obs)
	return nil
}

func (s *recordingObservationStore) Query(context.Context, observation.ObservationQuerySpec) ([]observation.ObservationRecord, error) {
	return nil, nil
}

func TestStoreBackedSink_CommitStampsObservationMetadata(t *testing.T) {
	store := &recordingObservationStore{}
	sink := StoreBackedSink{Observations: store}

	err := sink.Commit(context.Background(), "ex-1", []Effect{
		ObservationEffect{Observation: observation.ObservationRecord{
			RouteID:    "backend-a",
			ProviderID: "openai",
			ModelID:    "m",
			Code:       "obs.code",
			Reason:     "obs reason",
		}},
		LossEffect{
			Loss: report.Loss{
				ReasonCode: report.ReasonCode("loss_code"),
				Reason:     "loss reason",
				Field:      "field",
				Kind:       report.LossUnsupportedField,
				Severity:   report.SeverityWarning,
			},
			Observation: observation.ObservationRecord{
				RouteID:    "backend-a",
				ProviderID: "openai",
				ModelID:    "m",
			},
		},
	})
	if err != nil {
		t.Fatalf("Commit returned error: %v", err)
	}
	if len(store.records) != 2 {
		t.Fatalf("records len=%d want=2", len(store.records))
	}

	first := store.records[0]
	if first.RouteID != "backend-a" || first.ProviderID != "openai" || first.ModelID != "m" {
		t.Fatalf("first record route metadata = %#v, want backend-a/openai/m", first)
	}
	if first.Code != "obs.code" || first.Reason != "obs reason" {
		t.Fatalf("first record observation payload = %#v, want obs.code/obs reason", first)
	}
	if first.ObservedAt == 0 {
		t.Fatal("first record observed_at must be stamped")
	}

	second := store.records[1]
	if second.RouteID != "backend-a" || second.ProviderID != "openai" || second.ModelID != "m" {
		t.Fatalf("second record route metadata = %#v, want backend-a/openai/m", second)
	}
	if second.Code != "loss_code" || second.Reason != "loss reason" {
		t.Fatalf("second record loss payload = %#v, want loss_code/loss reason", second)
	}
	if second.ObservedAt == 0 {
		t.Fatal("second record observed_at must be stamped")
	}
}
