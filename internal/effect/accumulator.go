package effect

import "context"

// AccumulatorSink appends committed effects into a caller-owned slice.
//
// It is used at boundary seams that need to collect effect results locally
// before the exchange runner commits them to persistence.
type AccumulatorSink struct {
	Effects *[]Effect
}

func (s AccumulatorSink) Commit(_ context.Context, _ string, effects []Effect) error {
	if s.Effects == nil || len(effects) == 0 {
		return nil
	}
	*s.Effects = append(*s.Effects, effects...)
	return nil
}
