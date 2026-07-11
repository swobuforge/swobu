package transform

import (
	"bytes"
	"errors"
	"reflect"
	"slices"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/report"
)

// DocumentTransform applies one local wire-document mutation at one exchange stage.
type DocumentTransform interface {
	ID() string
	Stage() Stage
	Match(Context, carrier.WireDocument) bool
	Apply(Context, carrier.WireDocument) (carrier.WireDocument, Report, error)
}

// AppliedDocumentTransform is one executed transform result.
type AppliedDocumentTransform struct {
	ID           string
	Mutated      bool
	Losses       []report.Loss
	Notices      []NoticeRecord
	Observations []ObservationRecord
}

func (a AppliedDocumentTransform) Applied() bool {
	return a.ID != ""
}

// EventStreamTransform wraps one canonical event stream at one exchange stage.
type EventStreamTransform interface {
	ID() string
	Stage() Stage
	Match(Context, canonical.EventReader) bool
	Wrap(Context, canonical.EventReader) (canonical.EventReader, Report, error)
}

// AppliedEventStreamTransform is one executed stream wrapper result.
type AppliedEventStreamTransform struct {
	ID           string
	Mutated      bool
	Losses       []report.Loss
	Notices      []NoticeRecord
	Observations []ObservationRecord
}

func (a AppliedEventStreamTransform) Applied() bool {
	return a.ID != ""
}

// Registry provides deterministic stage-keyed transform chains.
type Registry struct {
	documentByStageCarrier map[registryKey][]DocumentTransform
	streamByStageCarrier   map[registryKey][]EventStreamTransform
}

type registryKey struct {
	Stage   Stage
	Carrier carrier.Kind
}

func NewRegistry(document []DocumentTransform, stream []EventStreamTransform) Registry {
	out := Registry{
		documentByStageCarrier: map[registryKey][]DocumentTransform{},
		streamByStageCarrier:   map[registryKey][]EventStreamTransform{},
	}
	for _, transform := range document {
		if transform == nil {
			continue
		}
		key := registryKey{Stage: transform.Stage(), Carrier: carrier.KindWireDocument}
		out.documentByStageCarrier[key] = append(out.documentByStageCarrier[key], transform)
	}
	for _, transform := range stream {
		if transform == nil {
			continue
		}
		key := registryKey{Stage: transform.Stage(), Carrier: carrier.KindCanonicalEventStream}
		out.streamByStageCarrier[key] = append(out.streamByStageCarrier[key], transform)
	}
	for key := range out.documentByStageCarrier {
		transforms := out.documentByStageCarrier[key]
		slices.SortStableFunc(transforms, func(a, b DocumentTransform) int {
			if a.ID() < b.ID() {
				return -1
			}
			if a.ID() > b.ID() {
				return 1
			}
			return 0
		})
		out.documentByStageCarrier[key] = transforms
	}
	for key := range out.streamByStageCarrier {
		transforms := out.streamByStageCarrier[key]
		slices.SortStableFunc(transforms, func(a, b EventStreamTransform) int {
			if a.ID() < b.ID() {
				return -1
			}
			if a.ID() > b.ID() {
				return 1
			}
			return 0
		})
		out.streamByStageCarrier[key] = transforms
	}
	return out
}

func (r Registry) ApplyDocument(ctx Context, doc carrier.WireDocument) (carrier.WireDocument, []AppliedDocumentTransform, error) {
	carrierKind := ctx.Carrier
	if carrierKind == "" {
		carrierKind = carrier.KindWireDocument
	}
	transforms := r.documentByStageCarrier[registryKey{Stage: ctx.Stage, Carrier: carrierKind}]
	if len(transforms) == 0 {
		return doc, nil, nil
	}
	out := doc
	applied := make([]AppliedDocumentTransform, 0, len(transforms))
	for _, transform := range transforms {
		if !transform.Match(ctx, out) {
			continue
		}
		next, outcome, err := transform.Apply(ctx, out)
		if err != nil {
			return carrier.WireDocument{}, nil, err
		}
		changed := wireDocumentChanged(out, next)
		if changed && !outcome.Mutated && len(outcome.Losses) == 0 {
			return carrier.WireDocument{}, nil, errors.New("document transform changed carrier without mutation or loss report")
		}
		if outcome.Mutated && !changed {
			return carrier.WireDocument{}, nil, errors.New("document transform reported mutation without carrier change")
		}
		out = next
		applied = append(applied, AppliedDocumentTransform{
			ID:           transform.ID(),
			Mutated:      outcome.Mutated,
			Losses:       append([]report.Loss(nil), outcome.Losses...),
			Notices:      append([]NoticeRecord(nil), outcome.Notices...),
			Observations: append([]ObservationRecord(nil), outcome.Observations...),
		})
	}
	return out, applied, nil
}

func (r Registry) WrapEventStream(ctx Context, reader canonical.EventReader) (canonical.EventReader, []AppliedEventStreamTransform, error) {
	carrierKind := ctx.Carrier
	if carrierKind == "" {
		carrierKind = carrier.KindCanonicalEventStream
	}
	transforms := r.streamByStageCarrier[registryKey{Stage: ctx.Stage, Carrier: carrierKind}]
	if len(transforms) == 0 {
		return reader, nil, nil
	}
	out := reader
	applied := make([]AppliedEventStreamTransform, 0, len(transforms))
	for _, transform := range transforms {
		if !transform.Match(ctx, out) {
			continue
		}
		next, outcome, err := transform.Wrap(ctx, out)
		if err != nil {
			return nil, nil, err
		}
		if outcome.Mutated && len(outcome.Mutations) == 0 && len(outcome.Losses) == 0 {
			return nil, nil, errors.New("event transform reported mutation without mutation or loss detail")
		}
		out = next
		applied = append(applied, AppliedEventStreamTransform{
			ID:           transform.ID(),
			Mutated:      outcome.Mutated,
			Losses:       append([]report.Loss(nil), outcome.Losses...),
			Notices:      append([]NoticeRecord(nil), outcome.Notices...),
			Observations: append([]ObservationRecord(nil), outcome.Observations...),
		})
	}
	return out, applied, nil
}

func wireDocumentChanged(before, after carrier.WireDocument) bool {
	if before.Stage != after.Stage || before.Family != after.Family || before.Media != after.Media {
		return true
	}
	if !reflect.DeepEqual(before.Header, after.Header) {
		return true
	}
	if !reflect.DeepEqual(before.Meta, after.Meta) {
		return true
	}
	return !bytes.Equal(before.RawBytes(), after.RawBytes())
}
