package exchange

import (
	"github.com/swobuforge/swobu/internal/report"
)

type StageReport = report.StageReport
type ProjectionLoss = report.Loss
type LossKind = report.LossKind
type Severity = report.Severity
type Notice = report.Notice
type Evidence = report.Evidence
type ExchangeReport = report.ExchangeReport

const (
	LossUnsupportedField            = report.LossUnsupportedField
	LossUnrepresentableTool         = report.LossUnrepresentableTool
	LossUnrepresentableContentPart  = report.LossUnrepresentableContentPart
	LossProviderPrivateStateMissing = report.LossProviderPrivateStateMissing

	SeverityNotice  = report.SeverityNotice
	SeverityWarning = report.SeverityWarning
	SeverityError   = report.SeverityError
)
