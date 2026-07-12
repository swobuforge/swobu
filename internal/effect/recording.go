package effect

import "context"

// RecordingSink stores every committed effect batch and optionally forwards it
// to a delegate sink. It is useful when a reader needs to surface effects only
// after the reader has been consumed.
type RecordingSink struct {
	Delegate Sink
	Effects  []Effect
}

func (s *RecordingSink) Commit(ctx context.Context, exchangeID string, effects []Effect) error {
	s.Effects = append(s.Effects, effects...)
	if s.Delegate != nil {
		return s.Delegate.Commit(ctx, exchangeID, effects)
	}
	return nil
}
