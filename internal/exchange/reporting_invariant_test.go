package exchange

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/effect"
	stage "github.com/swobuforge/swobu/internal/exchange/stage"
)

type invariantDocPatch struct {
	id      string
	mutated bool
	effects []effect.Effect
	nextRaw []byte
}

func (t invariantDocPatch) ID() string { return t.id }
func (t invariantDocPatch) Stage() stage.Stage {
	return stage.StageRequestDocumentOut
}
func (t invariantDocPatch) Capabilities() stage.StageCapabilities {
	return stage.StageCapabilities{}
}
func (t invariantDocPatch) Match(stage.Context, carrier.WireDocument) bool { return true }
func (t invariantDocPatch) Apply(_ stage.Context, in carrier.WireDocument) (stage.Result[carrier.WireDocument], error) {
	out := in
	out.Raw = append([]byte(nil), t.nextRaw...)
	return stage.Result[carrier.WireDocument]{
		Value:   out,
		Mutated: t.mutated,
		Effects: append([]effect.Effect(nil), t.effects...),
	}, nil
}

func TestApplyDocumentPatch_FailsOnSilentMutation(t *testing.T) {
	reg := stage.NewStageMechanics([]stage.DocumentPatch{
		invariantDocPatch{
			id:      "silent_change",
			mutated: false,
			nextRaw: []byte(`{"model":"m","input":"changed"}`),
		},
	}, nil)

	_, err := applyDocumentPatches(
		context.Background(),
		reg,
		"ex_invariant",
		stage.StageRequestDocumentOut,
		carrier.WireDocument{
			Stage:  carrier.StageProviderRequestOut,
			Family: canonical.ClientFamilyResponses,
			Media:  "application/json",
			Raw:    []byte(`{"model":"m","input":"hi"}`),
		},
		delivery.BufferedDelivery(),
	)
	if err == nil {
		t.Fatal("expected invariant error for silent mutation")
	}
}

func TestApplyDocumentPatch_FailsOnReportedMutationWithoutChange(t *testing.T) {
	reg := stage.NewStageMechanics([]stage.DocumentPatch{
		invariantDocPatch{
			id:      "false_mutation",
			mutated: true,
			nextRaw: []byte(`{"model":"m","input":"hi"}`),
		},
	}, nil)

	_, err := applyDocumentPatches(
		context.Background(),
		reg,
		"ex_invariant",
		stage.StageRequestDocumentOut,
		carrier.WireDocument{
			Stage:  carrier.StageProviderRequestOut,
			Family: canonical.ClientFamilyResponses,
			Media:  "application/json",
			Raw:    []byte(`{"model":"m","input":"hi"}`),
		},
		delivery.BufferedDelivery(),
	)
	if err == nil {
		t.Fatal("expected invariant error for reported mutation without change")
	}
}

func TestApplyDocumentPatch_PreservesRejectEffectOnError(t *testing.T) {
	rejectEffect := effect.CompatibilityEffect{
		Feature: compat.RequestStructuredOutput,
		Outcome: compat.Reject,
		Subject: compat.Subject("/input"),
	}
	reg := stage.NewStageMechanics([]stage.DocumentPatch{
		invariantDocPatch{
			id:      "lossy_change",
			mutated: false,
			effects: []effect.Effect{rejectEffect},
			nextRaw: []byte(`{"model":"m"}`),
		},
	}, nil)

	result, err := applyDocumentPatches(
		context.Background(),
		reg,
		"ex_invariant",
		stage.StageRequestDocumentOut,
		carrier.WireDocument{
			Stage:  carrier.StageProviderRequestOut,
			Family: canonical.ClientFamilyResponses,
			Media:  "application/json",
			Raw:    []byte(`{"model":"m","input":"hi"}`),
		},
		delivery.BufferedDelivery(),
	)
	if err == nil {
		t.Fatal("expected error for reject effect")
	}
	var unsupported UnsupportedProjectionError
	if !errors.As(err, &unsupported) {
		t.Fatalf("expected UnsupportedProjectionError, got %v", err)
	}
	if unsupported.Field != "/input" {
		t.Fatalf("unsupported field = %q, want /input", unsupported.Field)
	}
	if len(result.Effects) != 1 {
		t.Fatalf("result effects len=%d want=1", len(result.Effects))
	}
	gotReject, ok := result.Effects[0].(effect.CompatibilityEffect)
	if !ok {
		t.Fatalf("result effect type = %T, want effect.CompatibilityEffect", result.Effects[0])
	}
	if gotReject.Feature != compat.RequestStructuredOutput || gotReject.Outcome != compat.Reject || gotReject.Subject != compat.Subject("/input") {
		t.Fatalf("result reject effect = %#v, want structured_output/reject//input", gotReject)
	}
	if !strings.Contains(unsupported.Reason, "reject") {
		t.Fatalf("unsupported reason = %q, want reject detail", unsupported.Reason)
	}
}
