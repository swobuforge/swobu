package exchange

import (
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/executionevidence"
)

type exchangeSettlementCapture struct {
	settled executionevidence.ExchangeSettled
	count   int
}

func (s *exchangeSettlementCapture) AppendExecutionEvidence(event executionevidence.Event) error {
	if settled, ok := event.(executionevidence.ExchangeSettled); ok {
		s.settled = settled
		s.count++
	}
	return nil
}

func TestExchangeEvidenceStatusUsesActualDeliveryStatus(t *testing.T) {
	tests := []struct {
		name string
		kind delivery.ResultKind
		want int
	}{
		{name: "success", kind: delivery.Succeeded, want: 200},
		{name: "cancelled before headers", kind: delivery.ClientCancelled, want: 0},
		{name: "client write after headers", kind: delivery.ClientWriteFailed, want: 200},
		{name: "provider stream after accepted status", kind: delivery.ProviderStreamFailed, want: 202},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := execution.NewExchangeID()
			if err != nil {
				t.Fatal(err)
			}
			capture := &exchangeSettlementCapture{}
			settleExecutionEvidence(capture, id, delivery.Result{Kind: tt.kind, HTTPStatus: tt.want})
			if got := capture.settled.Status(); got != tt.want {
				t.Fatalf("status = %d, want %d", got, tt.want)
			}
			if capture.settled.SettledAt().After(time.Now()) {
				t.Fatal("settlement timestamp in future")
			}
		})
	}
}
