package transform

import (
	"context"
	"io"
	"net/http"
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

func TestRegistryApplyDocument_FailsOnSilentMutation(t *testing.T) {
	reg := NewRegistry([]DocumentTransform{
		silentMutationDocTransform{id: "mut"},
	}, nil)
	_, _, err := reg.ApplyDocument(Context{Stage: StageProviderWireOut}, carrier.WireDocument{
		Leg:    carrier.LegProviderRequestOut,
		Family: canonical.IngressFamilyResponses,
		Raw:    []byte(`{"model":"m","input":"hi"}`),
	})
	if err == nil {
		t.Fatal("expected error for silent mutation")
	}
}

func TestRegistryApplyDocument_FailsOnSilentHeaderMutation(t *testing.T) {
	reg := NewRegistry([]DocumentTransform{
		silentHeaderMutationDocTransform{id: "mut_header"},
	}, nil)
	_, _, err := reg.ApplyDocument(Context{Stage: StageProviderWireOut}, carrier.WireDocument{
		Leg:    carrier.LegProviderRequestOut,
		Family: canonical.IngressFamilyResponses,
		Header: http.Header{"X-A": {"1"}},
		Raw:    []byte(`{"model":"m","input":"hi"}`),
	})
	if err == nil {
		t.Fatal("expected error for silent header mutation")
	}
}

func TestRegistryApplyDocument_FailsOnSilentMetaMutation(t *testing.T) {
	reg := NewRegistry([]DocumentTransform{
		silentMetaMutationDocTransform{id: "mut_meta"},
	}, nil)
	_, _, err := reg.ApplyDocument(Context{Stage: StageProviderWireOut}, carrier.WireDocument{
		Leg:    carrier.LegProviderRequestOut,
		Family: canonical.IngressFamilyResponses,
		Meta:   carrier.Meta{ExchangeID: "ex1"},
		Raw:    []byte(`{"model":"m","input":"hi"}`),
	})
	if err == nil {
		t.Fatal("expected error for silent meta mutation")
	}
}

func TestRegistryWrapEventStream_FailsOnUndetailedMutation(t *testing.T) {
	reg := NewRegistry(nil, []EventStreamTransform{
		undetailedMutatingStreamTransform{id: "mut_stream"},
	})
	_, _, err := reg.WrapEventStream(Context{Stage: StageSemanticEvents}, canonical.NewSliceEventReader(nil))
	if err == nil {
		t.Fatal("expected error for undetailed event mutation")
	}
}

type testDocTransform struct {
	id string
}

func (t testDocTransform) ID() string                               { return t.id }
func (t testDocTransform) Stage() Stage                             { return StageProviderWireOut }
func (t testDocTransform) Match(Context, carrier.WireDocument) bool { return true }
func (t testDocTransform) Apply(_ Context, in carrier.WireDocument) (carrier.WireDocument, Report, error) {
	return in, Report{}, nil
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

type silentMutationDocTransform struct {
	id string
}

type silentHeaderMutationDocTransform struct {
	id string
}

func (t silentHeaderMutationDocTransform) ID() string                               { return t.id }
func (t silentHeaderMutationDocTransform) Stage() Stage                             { return StageProviderWireOut }
func (t silentHeaderMutationDocTransform) Match(Context, carrier.WireDocument) bool { return true }
func (t silentHeaderMutationDocTransform) Apply(_ Context, in carrier.WireDocument) (carrier.WireDocument, Report, error) {
	out := in
	out.Header = out.Header.Clone()
	out.Header.Set("X-A", "2")
	return out, Report{}, nil
}

type silentMetaMutationDocTransform struct {
	id string
}

func (t silentMetaMutationDocTransform) ID() string                               { return t.id }
func (t silentMetaMutationDocTransform) Stage() Stage                             { return StageProviderWireOut }
func (t silentMetaMutationDocTransform) Match(Context, carrier.WireDocument) bool { return true }
func (t silentMetaMutationDocTransform) Apply(_ Context, in carrier.WireDocument) (carrier.WireDocument, Report, error) {
	out := in
	out.Meta.ExchangeID = "ex2"
	return out, Report{}, nil
}

func (t silentMutationDocTransform) ID() string                               { return t.id }
func (t silentMutationDocTransform) Stage() Stage                             { return StageProviderWireOut }
func (t silentMutationDocTransform) Match(Context, carrier.WireDocument) bool { return true }
func (t silentMutationDocTransform) Apply(_ Context, in carrier.WireDocument) (carrier.WireDocument, Report, error) {
	out := in
	out.Raw = []byte(`{"model":"m","input":"changed"}`)
	return out, Report{}, nil
}

type undetailedMutatingStreamTransform struct {
	id string
}

func (t undetailedMutatingStreamTransform) ID() string                                { return t.id }
func (t undetailedMutatingStreamTransform) Stage() Stage                              { return StageSemanticEvents }
func (t undetailedMutatingStreamTransform) Match(Context, canonical.EventReader) bool { return true }
func (t undetailedMutatingStreamTransform) Wrap(_ Context, r canonical.EventReader) (canonical.EventReader, Report, error) {
	return r, Report{Mutated: true}, nil
}

type eofReader struct{}

func (eofReader) Next(context.Context) (canonical.Event, error) { return canonical.Event{}, io.EOF }
func (eofReader) Close(context.Context) error                   { return nil }
