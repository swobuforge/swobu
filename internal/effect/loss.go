package effect

import (
	"github.com/swobuforge/swobu/internal/observation"
	"github.com/swobuforge/swobu/internal/report"
)

type LossEffect struct {
	Loss        report.Loss
	Observation observation.ObservationRecord
}

func (LossEffect) Kind() Kind { return KindLoss }
