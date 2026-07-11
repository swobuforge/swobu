package effect

import "github.com/swobuforge/swobu/internal/report"

type LossEffect struct {
	Loss report.Loss
}

func (LossEffect) Kind() Kind { return KindLoss }
