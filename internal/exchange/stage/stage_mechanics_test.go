package stage

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestStageMechanicsApplyDocument_DeterministicOrderByID(t *testing.T) {
	reg := NewStageMechanics([]DocumentPatch{
		testDocPatch{id: "b"},
		testDocPatch{id: "a"},
	}, nil)
	_, applied, err := reg.ApplyDocument(StageRequestDocumentOut, Context{}, carrier.CarrierDocument{})
	if err != nil {
		t.Fatalf("ApplyDocument() error = %v", err)
	}
	if len(applied) != 2 || applied[0].ID != "a" || applied[1].ID != "b" {
		t.Fatalf("applied order = %#v", applied)
	}
}

func TestStageMechanicsWrapEventStream_DeterministicOrderByID(t *testing.T) {
	reg := NewStageMechanics(nil, []EventStreamWrapper{
		testStreamWrapper{id: "z"},
		testStreamWrapper{id: "a"},
	})
	_, applied, err := reg.WrapEventStream(StageSemanticEvents, Context{}, canonical.NewSliceEventReader(nil))
	if err != nil {
		t.Fatalf("WrapEventStream() error = %v", err)
	}
	if len(applied) != 2 || applied[0].ID != "a" || applied[1].ID != "z" {
		t.Fatalf("applied order = %#v", applied)
	}
}

func TestStageMechanicsWrapEventStream_IdentityWhenNoWrappers(t *testing.T) {
	reg := NewStageMechanics(nil, nil)
	in := canonical.NewSliceEventReader(nil)
	out, applied, err := reg.WrapEventStream(StageSemanticEvents, Context{}, in)
	if err != nil {
		t.Fatalf("WrapEventStream() error = %v", err)
	}
	if out == nil || len(applied) != 0 {
		t.Fatalf("unexpected output: out=%v applied=%#v", out, applied)
	}
}

func TestStageMechanicsWrapEventStream_CarriesCapabilities(t *testing.T) {
	reg := NewStageMechanics(nil, []EventStreamWrapper{
		bufferingStreamWrapper{id: "buffer", caps: StageCapabilities{BuffersResponse: true}},
	})
	_, applied, err := reg.WrapEventStream(StageSemanticEvents, Context{}, canonical.NewSliceEventReader(nil))
	if err != nil {
		t.Fatalf("WrapEventStream() error = %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("applied len=%d want 1", len(applied))
	}
	if !applied[0].Capabilities.BuffersResponse {
		t.Fatalf("applied capabilities = %#v, want BuffersResponse", applied[0].Capabilities)
	}
}

func TestStageMechanicsApplyDocument_DoesNotApplyOnCarrierMismatch(t *testing.T) {
	reg := NewStageMechanics([]DocumentPatch{
		testDocPatch{id: "a"},
	}, nil)
	_, applied, err := reg.ApplyDocument(StageRequestDocumentOut, Context{
		Carrier: carrier.KindCanonicalEventStream,
	}, carrier.CarrierDocument{})
	if err != nil {
		t.Fatalf("ApplyDocument() error = %v", err)
	}
	if len(applied) != 0 {
		t.Fatalf("applied=%#v, want none", applied)
	}
}

func TestStageMechanicsApplyDocument_FailsOnSilentMutation(t *testing.T) {
	reg := NewStageMechanics([]DocumentPatch{
		silentMutationDocPatch{id: "mut"},
	}, nil)
	_, _, err := reg.ApplyDocument(StageRequestDocumentOut, Context{}, carrier.CarrierDocument{
		Stage:  carrier.StageProviderRequestOut,
		Family: canonical.ClientFamilyResponses,
		Raw:    []byte(`{"model":"m","input":"hi"}`),
	})
	if err == nil {
		t.Fatal("expected error for silent mutation")
	}
}

func TestStageMechanicsApplyDocument_FailsOnSilentHeaderMutation(t *testing.T) {
	reg := NewStageMechanics([]DocumentPatch{
		silentHeaderMutationDocPatch{id: "mut_header"},
	}, nil)
	_, _, err := reg.ApplyDocument(StageRequestDocumentOut, Context{}, carrier.CarrierDocument{
		Stage:  carrier.StageProviderRequestOut,
		Family: canonical.ClientFamilyResponses,
		Header: http.Header{"X-A": {"1"}},
		Raw:    []byte(`{"model":"m","input":"hi"}`),
	})
	if err == nil {
		t.Fatal("expected error for silent header mutation")
	}
}

func TestStageMechanicsApplyDocument_FailsOnSilentMetaMutation(t *testing.T) {
	reg := NewStageMechanics([]DocumentPatch{
		silentMetaMutationDocPatch{id: "mut_meta"},
	}, nil)
	_, _, err := reg.ApplyDocument(StageRequestDocumentOut, Context{}, carrier.CarrierDocument{
		Stage:  carrier.StageProviderRequestOut,
		Family: canonical.ClientFamilyResponses,
		Meta:   carrier.Meta{BackendRef: "ex1"},
		Raw:    []byte(`{"model":"m","input":"hi"}`),
	})
	if err == nil {
		t.Fatal("expected error for silent meta mutation")
	}
}

func TestStageMechanicsWrapEventStream_FailsOnUndetailedMutation(t *testing.T) {
	reg := NewStageMechanics(nil, []EventStreamWrapper{
		undetailedMutatingStreamWrapper{id: "mut_stream"},
	})
	_, _, err := reg.WrapEventStream(StageSemanticEvents, Context{}, canonical.NewSliceEventReader(nil))
	if err == nil {
		t.Fatal("expected error for undetailed event mutation")
	}
}

type testDocPatch struct {
	id string
}

func (t testDocPatch) ID() string                                  { return t.id }
func (t testDocPatch) Stage() Stage                                { return StageRequestDocumentOut }
func (t testDocPatch) Capabilities() StageCapabilities             { return StageCapabilities{} }
func (t testDocPatch) Match(Context, carrier.CarrierDocument) bool { return true }
func (t testDocPatch) Apply(_ Context, in carrier.CarrierDocument) (Result[carrier.CarrierDocument], error) {
	return Result[carrier.CarrierDocument]{Value: in}, nil
}

type testStreamWrapper struct {
	id string
}

func (t testStreamWrapper) ID() string                                { return t.id }
func (t testStreamWrapper) Stage() Stage                              { return StageSemanticEvents }
func (t testStreamWrapper) Capabilities() StageCapabilities           { return StageCapabilities{} }
func (t testStreamWrapper) Match(Context, canonical.EventReader) bool { return true }
func (t testStreamWrapper) Wrap(_ Context, r canonical.EventReader) (Result[canonical.EventReader], error) {
	return Result[canonical.EventReader]{Value: r}, nil
}

type silentMutationDocPatch struct {
	id string
}

type silentHeaderMutationDocPatch struct {
	id string
}

func (t silentHeaderMutationDocPatch) ID() string   { return t.id }
func (t silentHeaderMutationDocPatch) Stage() Stage { return StageRequestDocumentOut }
func (t silentHeaderMutationDocPatch) Capabilities() StageCapabilities {
	return StageCapabilities{}
}
func (t silentHeaderMutationDocPatch) Match(Context, carrier.CarrierDocument) bool {
	return true
}
func (t silentHeaderMutationDocPatch) Apply(_ Context, in carrier.CarrierDocument) (Result[carrier.CarrierDocument], error) {
	out := in
	out.Header = out.Header.Clone()
	out.Header.Set("X-A", "2")
	return Result[carrier.CarrierDocument]{Value: out}, nil
}

type silentMetaMutationDocPatch struct {
	id string
}

func (t silentMetaMutationDocPatch) ID() string   { return t.id }
func (t silentMetaMutationDocPatch) Stage() Stage { return StageRequestDocumentOut }
func (t silentMetaMutationDocPatch) Capabilities() StageCapabilities {
	return StageCapabilities{}
}
func (t silentMetaMutationDocPatch) Match(Context, carrier.CarrierDocument) bool { return true }
func (t silentMetaMutationDocPatch) Apply(_ Context, in carrier.CarrierDocument) (Result[carrier.CarrierDocument], error) {
	out := in
	out.Meta.BackendRef = "ex2"
	return Result[carrier.CarrierDocument]{Value: out}, nil
}

func (t silentMutationDocPatch) ID() string   { return t.id }
func (t silentMutationDocPatch) Stage() Stage { return StageRequestDocumentOut }
func (t silentMutationDocPatch) Capabilities() StageCapabilities {
	return StageCapabilities{}
}
func (t silentMutationDocPatch) Match(Context, carrier.CarrierDocument) bool { return true }
func (t silentMutationDocPatch) Apply(_ Context, in carrier.CarrierDocument) (Result[carrier.CarrierDocument], error) {
	out := in
	out.Raw = []byte(`{"model":"m","input":"changed"}`)
	return Result[carrier.CarrierDocument]{Value: out}, nil
}

type undetailedMutatingStreamWrapper struct {
	id string
}

func (t undetailedMutatingStreamWrapper) ID() string   { return t.id }
func (t undetailedMutatingStreamWrapper) Stage() Stage { return StageSemanticEvents }
func (t undetailedMutatingStreamWrapper) Capabilities() StageCapabilities {
	return StageCapabilities{}
}
func (t undetailedMutatingStreamWrapper) Match(Context, canonical.EventReader) bool { return true }
func (t undetailedMutatingStreamWrapper) Wrap(_ Context, r canonical.EventReader) (Result[canonical.EventReader], error) {
	return Result[canonical.EventReader]{Value: r, Mutated: true}, nil
}

type bufferingStreamWrapper struct {
	id   string
	caps StageCapabilities
}

func (t bufferingStreamWrapper) ID() string                                { return t.id }
func (t bufferingStreamWrapper) Stage() Stage                              { return StageSemanticEvents }
func (t bufferingStreamWrapper) Capabilities() StageCapabilities           { return t.caps }
func (t bufferingStreamWrapper) Match(Context, canonical.EventReader) bool { return true }
func (t bufferingStreamWrapper) Wrap(_ Context, r canonical.EventReader) (Result[canonical.EventReader], error) {
	return Result[canonical.EventReader]{Value: r}, nil
}

type eofReader struct{}

func (eofReader) Next(context.Context) (canonical.Event, error) { return canonical.Event{}, io.EOF }
func (eofReader) Close(context.Context) error                   { return nil }
