package transform

import (
	"context"
	"io"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestRegistryApplyDocument_DeterministicOrderByID(t *testing.T) {
	reg := NewRegistry([]DocumentTransform{
		testDocTransform{id: "b"},
		testDocTransform{id: "a"},
	}, nil)
	_, applied, err := reg.ApplyDocument(Context{Stage: StageProviderWireOut}, carrier.WireDocument{})
	if err != nil {
		t.Fatalf("ApplyDocument() error = %v", err)
	}
	if len(applied) != 2 || applied[0].ID != "a" || applied[1].ID != "b" {
		t.Fatalf("applied order = %#v", applied)
	}
}

func TestRegistryWrapEventStream_DeterministicOrderByID(t *testing.T) {
	reg := NewRegistry(nil, []EventStreamTransform{
		testStreamTransform{id: "z"},
		testStreamTransform{id: "a"},
	})
	_, applied, err := reg.WrapEventStream(Context{Stage: StageSemanticEvents}, canonical.NewSliceEventReader(nil))
	if err != nil {
		t.Fatalf("WrapEventStream() error = %v", err)
	}
	if len(applied) != 2 || applied[0].ID != "a" || applied[1].ID != "z" {
		t.Fatalf("applied order = %#v", applied)
	}
}

func TestRegistryWrapEventStream_IdentityWhenNoTransforms(t *testing.T) {
	reg := NewRegistry(nil, nil)
	in := canonical.NewSliceEventReader(nil)
	out, applied, err := reg.WrapEventStream(Context{Stage: StageSemanticEvents}, in)
	if err != nil {
		t.Fatalf("WrapEventStream() error = %v", err)
	}
	if out == nil || len(applied) != 0 {
		t.Fatalf("unexpected output: out=%v applied=%#v", out, applied)
	}
}

func TestRegistryApplyDocument_DoesNotApplyOnCarrierMismatch(t *testing.T) {
	reg := NewRegistry([]DocumentTransform{
		testDocTransform{id: "a"},
	}, nil)
	_, applied, err := reg.ApplyDocument(Context{
		Stage:   StageProviderWireOut,
		Carrier: carrier.KindCanonicalEventStream,
	}, carrier.WireDocument{})
	if err != nil {
		t.Fatalf("ApplyDocument() error = %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("applied=%#v, want none", applied)
	}
}

type testDocTransform struct {
	id string
}

func (t testDocTransform) ID() string                               { return t.id }
func (t testDocTransform) Stage() Stage                             { return StageProviderWireOut }
func (t testDocTransform) Match(Context, carrier.WireDocument) bool { return true }
func (t testDocTransform) Apply(Context, carrier.WireDocument) (carrier.WireDocument, Report, error) {
	return carrier.WireDocument{}, Report{}, nil
}

type testStreamTransform struct {
	id string
}

func (t testStreamTransform) ID() string                                { return t.id }
func (t testStreamTransform) Stage() Stage                              { return StageSemanticEvents }
func (t testStreamTransform) Match(Context, canonical.EventReader) bool { return true }
func (t testStreamTransform) Wrap(_ Context, r canonical.EventReader) (canonical.EventReader, Report, error) {
	return r, Report{}, nil
}

type eofReader struct{}

func (eofReader) Next(context.Context) (canonical.Event, error) { return canonical.Event{}, io.EOF }
func (eofReader) Close(context.Context) error                   { return nil }
