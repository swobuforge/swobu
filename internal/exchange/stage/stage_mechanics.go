package stage

import (
	"bytes"
	"errors"
	"reflect"
	"slices"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/effect"
)

// DocumentPatch applies one local wire-document patch at one exchange stage.
type DocumentPatch interface {
	ID() string
	Stage() Stage
	Capabilities() StageCapabilities
	Match(Context, carrier.CarrierDocument) bool
	Apply(Context, carrier.CarrierDocument) (Result[carrier.CarrierDocument], error)
}

// AppliedDocumentPatch is one executed document patch result.
type AppliedDocumentPatch struct {
	ID           string
	Mutated      bool
	Capabilities StageCapabilities
	Effects      []effect.Effect
}

func (a AppliedDocumentPatch) Applied() bool {
	return a.ID != ""
}

// EventStreamWrapper wraps one canonical event stream at one exchange stage.
type EventStreamWrapper interface {
	ID() string
	Stage() Stage
	Capabilities() StageCapabilities
	Match(Context, canonical.EventReader) bool
	Wrap(Context, canonical.EventReader) (Result[canonical.EventReader], error)
}

// AppliedEventStreamWrapper is one executed stream wrapper result.
type AppliedEventStreamWrapper struct {
	ID           string
	Mutated      bool
	Capabilities StageCapabilities
	Effects      []effect.Effect
}

func (a AppliedEventStreamWrapper) Applied() bool {
	return a.ID != ""
}

// StageMechanics provides deterministic stage-keyed patch and wrapper chains.
type StageMechanics struct {
	documentByStageCarrier map[registryKey][]DocumentPatch
	streamByStageCarrier   map[registryKey][]EventStreamWrapper
}

type registryKey struct {
	Stage   Stage
	Carrier carrier.Kind
}

func NewStageMechanics(document []DocumentPatch, stream []EventStreamWrapper) StageMechanics {
	out := StageMechanics{
		documentByStageCarrier: map[registryKey][]DocumentPatch{},
		streamByStageCarrier:   map[registryKey][]EventStreamWrapper{},
	}
	for _, patch := range document {
		if patch == nil {
			continue
		}
		key := registryKey{Stage: patch.Stage(), Carrier: carrier.KindCarrierDocument}
		out.documentByStageCarrier[key] = append(out.documentByStageCarrier[key], patch)
	}
	for _, wrapper := range stream {
		if wrapper == nil {
			continue
		}
		key := registryKey{Stage: wrapper.Stage(), Carrier: carrier.KindCanonicalEventStream}
		out.streamByStageCarrier[key] = append(out.streamByStageCarrier[key], wrapper)
	}
	for key := range out.documentByStageCarrier {
		patches := out.documentByStageCarrier[key]
		slices.SortStableFunc(patches, func(a, b DocumentPatch) int {
			if a.ID() < b.ID() {
				return -1
			}
			if a.ID() > b.ID() {
				return 1
			}
			return 0
		})
		out.documentByStageCarrier[key] = patches
	}
	for key := range out.streamByStageCarrier {
		wrappers := out.streamByStageCarrier[key]
		slices.SortStableFunc(wrappers, func(a, b EventStreamWrapper) int {
			if a.ID() < b.ID() {
				return -1
			}
			if a.ID() > b.ID() {
				return 1
			}
			return 0
		})
		out.streamByStageCarrier[key] = wrappers
	}
	return out
}

// ApplyDocument applies the stage-selected document patches for one carrier kind.
func (r StageMechanics) ApplyDocument(stage Stage, ctx Context, doc carrier.CarrierDocument) (carrier.CarrierDocument, []AppliedDocumentPatch, error) {
	carrierKind := ctx.Carrier
	if carrierKind == "" {
		carrierKind = carrier.KindCarrierDocument
	}
	patches := r.documentByStageCarrier[registryKey{Stage: stage, Carrier: carrierKind}]
	if len(patches) == 0 {
		return doc, nil, nil
	}
	out := doc
	applied := make([]AppliedDocumentPatch, 0, len(patches))
	for _, patch := range patches {
		if !patch.Match(ctx, out) {
			continue
		}
		result, err := patch.Apply(ctx, out)
		effects := cloneEffects(result.Effects)
		if err != nil {
			applied = append(applied, AppliedDocumentPatch{
				ID:           patch.ID(),
				Mutated:      result.Mutated,
				Capabilities: patch.Capabilities(),
				Effects:      effects,
			})
			return carrier.CarrierDocument{}, applied, err
		}
		changed := wireDocumentChanged(out, result.Value)
		if changed && !result.Mutated && len(effects) == 0 {
			return carrier.CarrierDocument{}, nil, errors.New("document patch changed carrier without mutation or effect detail")
		}
		if result.Mutated && !changed {
			return carrier.CarrierDocument{}, nil, errors.New("document patch reported mutation without carrier change")
		}
		out = result.Value
		applied = append(applied, AppliedDocumentPatch{
			ID:           patch.ID(),
			Mutated:      result.Mutated,
			Capabilities: patch.Capabilities(),
			Effects:      effects,
		})
	}
	return out, applied, nil
}

// WrapEventStream applies the stage-selected stream wrappers for one carrier kind.
func (r StageMechanics) WrapEventStream(stage Stage, ctx Context, reader canonical.EventReader) (canonical.EventReader, []AppliedEventStreamWrapper, error) {
	carrierKind := ctx.Carrier
	if carrierKind == "" {
		carrierKind = carrier.KindCanonicalEventStream
	}
	wrappers := r.streamByStageCarrier[registryKey{Stage: stage, Carrier: carrierKind}]
	if len(wrappers) == 0 {
		return reader, nil, nil
	}
	out := reader
	applied := make([]AppliedEventStreamWrapper, 0, len(wrappers))
	for _, wrapper := range wrappers {
		if !wrapper.Match(ctx, out) {
			continue
		}
		result, err := wrapper.Wrap(ctx, out)
		effects := cloneEffects(result.Effects)
		if err != nil {
			applied = append(applied, AppliedEventStreamWrapper{
				ID:           wrapper.ID(),
				Mutated:      result.Mutated,
				Capabilities: wrapper.Capabilities(),
				Effects:      effects,
			})
			return nil, applied, err
		}
		if result.Mutated && len(effects) == 0 {
			return nil, nil, errors.New("event wrapper reported mutation without mutation or effect detail")
		}
		out = result.Value
		applied = append(applied, AppliedEventStreamWrapper{
			ID:           wrapper.ID(),
			Mutated:      result.Mutated,
			Capabilities: wrapper.Capabilities(),
			Effects:      effects,
		})
	}
	return out, applied, nil
}

func cloneEffects(src []effect.Effect) []effect.Effect {
	if len(src) == 0 {
		return nil
	}
	return append([]effect.Effect(nil), src...)
}

func wireDocumentChanged(before, after carrier.CarrierDocument) bool {
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
