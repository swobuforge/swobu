package exchange

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/execution"
)

type failingAfterTerminalStream struct {
	events []canonical.Event
	index  int
}

type closeUnblocksNextStream struct {
	reading chan struct{}
	closed  chan struct{}
}

type continuousUsageStream struct{}

func (continuousUsageStream) Next(context.Context) (canonical.Event, error) {
	return canonical.Event{Kind: canonical.EventUsage}, nil
}

func (continuousUsageStream) Close(context.Context) error { return nil }

func TestProviderEvidenceUsageAndCloseRace(t *testing.T) {
	stream := newProviderEvidenceStream(continuousUsageStream{}, func(execution.Outcome, canonical.TokenUsage) {})
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		<-start
		for range 10000 {
			_, _ = stream.Next(context.Background())
		}
	}()
	go func() {
		defer workers.Done()
		<-start
		_ = stream.Close(context.Background())
	}()
	close(start)
	workers.Wait()
}

func TestProviderEvidenceCloseWaitsForWinningTerminalCallback(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	stream := newProviderEvidenceStream(continuousUsageStream{}, func(execution.Outcome, canonical.TokenUsage) {
		close(entered)
		<-release
	})
	firstDone := make(chan struct{})
	go func() {
		stream.(*providerEvidenceStream).settleTerminal(execution.Succeeded, canonical.NewUnknownTokenUsage())
		close(firstDone)
	}()
	<-entered
	closed := make(chan struct{})
	go func() {
		_ = stream.Close(context.Background())
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("Close returned before the winning terminal callback completed")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	<-firstDone
	<-closed
}

func (s *closeUnblocksNextStream) Next(context.Context) (canonical.Event, error) {
	close(s.reading)
	<-s.closed
	return canonical.Event{}, context.Canceled
}

func (s *closeUnblocksNextStream) Close(context.Context) error {
	close(s.closed)
	return nil
}

func TestProviderEvidenceConcurrentNextAndCloseSettlesCancellationOnce(t *testing.T) {
	upstream := &closeUnblocksNextStream{reading: make(chan struct{}), closed: make(chan struct{})}
	var mu sync.Mutex
	var outcomes []execution.Outcome
	stream := newProviderEvidenceStream(upstream, func(outcome execution.Outcome, _ canonical.TokenUsage) {
		mu.Lock()
		defer mu.Unlock()
		outcomes = append(outcomes, outcome)
	})
	nextDone := make(chan error, 1)
	go func() {
		_, err := stream.Next(context.Background())
		nextDone <- err
	}()
	<-upstream.reading
	if err := stream.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-nextDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("Next error = %v, want cancellation", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(outcomes) != 1 || outcomes[0] != execution.Cancelled {
		t.Fatalf("settlement outcomes = %v, want one cancellation", outcomes)
	}
}

func (s *failingAfterTerminalStream) Next(context.Context) (canonical.Event, error) {
	if s.index < len(s.events) {
		event := s.events[s.index]
		s.index++
		return event, nil
	}
	return canonical.Event{}, errors.New("client-side consumer failed after provider terminal")
}
func (*failingAfterTerminalStream) Close(context.Context) error { return nil }

func TestProviderEvidenceSettlesBeforeDownstreamDeliveryFailure(t *testing.T) {
	events := canonical.SynthesizeResponseEnvelopeEvents("exchange", canonical.ResponseRef{SwobuID: "response"}, "model", nil, canonical.Completed("stop"), canonical.NewUnknownTokenUsage())
	var settled bool
	stream := newProviderEvidenceStream(&failingAfterTerminalStream{events: events}, func(outcome execution.Outcome, _ canonical.TokenUsage) {
		settled = outcome == execution.Succeeded
	})
	for {
		_, err := stream.Next(context.Background())
		if err != nil {
			if !settled {
				t.Fatal("provider evidence was not settled at canonical terminal")
			}
			return
		}
	}
}

func TestProviderSuccessRemainsSucceededAfterClientCancellation(t *testing.T) {
	id, err := execution.NewExchangeID()
	if err != nil {
		t.Fatal(err)
	}
	providerEvents := canonical.SynthesizeResponseEnvelopeEvents("exchange", canonical.ResponseRef{SwobuID: "response"}, "model", nil, canonical.Completed("stop"), canonical.NewUnknownTokenUsage())
	var outcomes []execution.Outcome
	stream := newProviderEvidenceStream(&failingAfterTerminalStream{events: providerEvents}, func(outcome execution.Outcome, _ canonical.TokenUsage) {
		outcomes = append(outcomes, outcome)
	})
	for range providerEvents {
		if _, err := stream.Next(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if len(outcomes) != 1 || outcomes[0] != execution.Succeeded {
		t.Fatalf("provider terminal outcomes before client disconnect = %v", outcomes)
	}
	if err := stream.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 1 || outcomes[0] != execution.Succeeded {
		t.Fatalf("client disconnect rewrote provider terminal: %v", outcomes)
	}
	capture := &exchangeSettlementCapture{}
	settleExecutionEvidence(capture, id, delivery.Result{Kind: delivery.ClientCancelled})
	if capture.count != 1 || capture.settled.ID() != id || capture.settled.Result() != delivery.ClientCancelled || capture.settled.Status() != 0 {
		t.Fatalf("delivery-owned cancellation after provider success = %#v", capture.settled)
	}
	if len(outcomes) != 1 || outcomes[0] != execution.Succeeded {
		t.Fatalf("Exchange cancellation rewrote provider terminal: %v", outcomes)
	}
}

func TestProviderEvidenceEarlyCloseSettlesCancellationOnce(t *testing.T) {
	events := canonical.SynthesizeResponseEnvelopeEvents("exchange", canonical.ResponseRef{SwobuID: "response"}, "model", nil, canonical.Completed("stop"), canonical.NewUnknownTokenUsage())
	var outcomes []execution.Outcome
	stream := newProviderEvidenceStream(&failingAfterTerminalStream{events: events}, func(outcome execution.Outcome, _ canonical.TokenUsage) {
		outcomes = append(outcomes, outcome)
	})
	if _, err := stream.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = stream.Close(context.Background())
	_ = stream.Close(context.Background())
	if len(outcomes) != 1 || outcomes[0] != execution.Cancelled {
		t.Fatalf("early close outcomes = %v, want one cancellation", outcomes)
	}
}

func TestProviderEvidence1000ConcurrentStreamCancellationsSettleOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("streaming cancellation qualification stress")
	}
	const count = 1000
	events := canonical.SynthesizeResponseEnvelopeEvents("exchange", canonical.ResponseRef{SwobuID: "response"}, "model", nil, canonical.Completed("stop"), canonical.NewUnknownTokenUsage())
	var workers sync.WaitGroup
	for i := 0; i < count; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			var settlements int
			stream := newProviderEvidenceStream(&failingAfterTerminalStream{events: events}, func(outcome execution.Outcome, _ canonical.TokenUsage) {
				settlements++
				if outcome != execution.Cancelled {
					t.Errorf("early close settled %q, want cancelled", outcome)
				}
			})
			if _, err := stream.Next(context.Background()); err != nil {
				t.Errorf("first event: %v", err)
				return
			}
			_ = stream.Close(context.Background())
			_ = stream.Close(context.Background())
			if settlements != 1 {
				t.Errorf("settlements = %d, want 1", settlements)
			}
		}()
	}
	workers.Wait()
}

func TestProviderEvidenceStreamErrorSettlesFailureOnce(t *testing.T) {
	var outcomes []execution.Outcome
	stream := newProviderEvidenceStream(&failingAfterTerminalStream{}, func(outcome execution.Outcome, _ canonical.TokenUsage) {
		outcomes = append(outcomes, outcome)
	})
	_, _ = stream.Next(context.Background())
	_ = stream.Close(context.Background())
	if len(outcomes) != 1 || outcomes[0] != execution.Failed {
		t.Fatalf("stream error outcomes = %v, want one failure", outcomes)
	}
}

type cancelledProviderStream struct{}

func (cancelledProviderStream) Next(context.Context) (canonical.Event, error) {
	return canonical.Event{}, context.Canceled
}
func (cancelledProviderStream) Close(context.Context) error { return context.Canceled }

func TestProviderEvidenceNextCancellationSettlesOnce(t *testing.T) {
	var outcomes []execution.Outcome
	stream := newProviderEvidenceStream(cancelledProviderStream{}, func(outcome execution.Outcome, _ canonical.TokenUsage) {
		outcomes = append(outcomes, outcome)
	})
	_, _ = stream.Next(context.Background())
	_ = stream.Close(context.Background())
	if len(outcomes) != 1 || outcomes[0] != execution.Cancelled {
		t.Fatalf("provider Next cancellation outcomes = %v", outcomes)
	}
}

func TestProviderEvidenceCloseCancellationSettlesOnce(t *testing.T) {
	var outcomes []execution.Outcome
	stream := newProviderEvidenceStream(cancelledProviderStream{}, func(outcome execution.Outcome, _ canonical.TokenUsage) {
		outcomes = append(outcomes, outcome)
	})
	if err := stream.Close(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatalf("provider Close error = %v, want cancellation", err)
	}
	_ = stream.Close(context.Background())
	if len(outcomes) != 1 || outcomes[0] != execution.Cancelled {
		t.Fatalf("provider Close cancellation outcomes = %v", outcomes)
	}
}
