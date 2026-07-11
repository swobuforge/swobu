package effect

import "github.com/swobuforge/swobu/internal/observation"

// ObservationEffect carries one runtime observation with route identity already
// resolved from the actual exchange execution path.
type ObservationEffect struct {
	Observation observation.ObservationRecord
}

func (ObservationEffect) Kind() Kind { return KindObservation }
