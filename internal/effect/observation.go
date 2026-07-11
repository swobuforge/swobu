package effect

import "github.com/swobuforge/swobu/internal/observation"

type ObservationEffect struct {
	Observation observation.ObservationRecord
}

func (ObservationEffect) Kind() Kind { return KindObservation }
