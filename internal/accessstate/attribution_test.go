package accessstate

import (
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"testing"
)

func TestDelegatedPrincipalCreationReceiptsAreCallerScoped(t *testing.T) {
	store, err := OpenInline(nil, func([]byte) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	first := accessidentity.NewManaged([16]byte{1})
	second := accessidentity.NewManaged([16]byte{2})
	firstContext := accessidentity.WithAttribution(t.Context(), first)
	secondContext := accessidentity.WithAttribution(t.Context(), second)
	allow := func() error { return nil }
	a, replayed, err := store.CreatePrincipalChecked(firstContext, "First target", "same-client-key", allow)
	if err != nil || replayed {
		t.Fatal("first creation failed")
	}
	b, replayed, err := store.CreatePrincipalChecked(secondContext, "Second target", "same-client-key", allow)
	if err != nil || replayed || a.ID == b.ID {
		t.Fatal("admitted administrators collided on a retry key")
	}
	again, replayed, err := store.CreatePrincipalChecked(firstContext, "First target", "same-client-key", allow)
	if err != nil || !replayed || again.ID != a.ID {
		t.Fatal("original caller lost its receipt")
	}
	audits := store.PrincipalCreationAudits()
	if len(audits) != 2 || audits[0].Caller != first.String() || audits[1].Caller != second.String() {
		t.Fatal("durable audit lost original caller")
	}
}
