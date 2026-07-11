package transform

import "github.com/swobuforge/swobu/internal/report"

type MutationRecord struct {
	Field  string
	Reason string
}

type NoticeRecord struct {
	Code   string
	Field  string
	Reason string
}

type ObservationRecord struct {
	Code   string
	Field  string
	Reason string
}

// Outcome is one transform execution outcome.
type Outcome struct {
	Mutated      bool
	Mutations    []MutationRecord
	Losses       []report.Loss
	Notices      []NoticeRecord
	Observations []ObservationRecord
}
