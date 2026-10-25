package limits

import (
	"context"
	"errors"
	"github.com/swobuforge/swobu/internal/accounting"
	"github.com/swobuforge/swobu/internal/app/operator/adminapproval"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"path/filepath"
	"testing"
)

func TestReviewBindsAdmittedActorAndRepeatsAdmissionAtEffect(t *testing.T) {
	store, err := accounting.Open(filepath.Join(t.TempDir(), "accounting.json"))
	if err != nil {
		t.Fatal(err)
	}
	service := New(store, adminapproval.New(nil), func(workspace string) bool { return workspace == "research" })
	first := accessidentity.WithAttribution(t.Context(), accessidentity.NewManaged([16]byte{1}))
	second := accessidentity.WithAttribution(t.Context(), accessidentity.NewManaged([16]byte{2}))
	allow := func(context.Context) error { return nil }
	limit := accounting.Limit{Currency: "USD", AmountCents: 100, Window: "day", Mode: accounting.ModeObserveOnly, UnknownPrice: "reject"}
	review, err := service.Preview(first, "research", limit, allow)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Apply(second, "research", review.ID, limit, allow); err == nil {
		t.Fatal("copied review accepted another admitted actor")
	}
	if _, ok := store.Limit("research"); ok {
		t.Fatal("foreign review changed policy")
	}
	if err := service.Apply(first, "research", review.ID, limit, allow); err != nil {
		t.Fatal(err)
	}
	limit.AmountCents = 200
	review, err = service.Preview(first, "research", limit, allow)
	if err != nil {
		t.Fatal(err)
	}
	checks := 0
	revoke := func(context.Context) error {
		checks++
		if checks > 1 {
			return errors.New("authority revoked")
		}
		return nil
	}
	if err := service.Apply(first, "research", review.ID, limit, revoke); err == nil {
		t.Fatal("revoked effect changed policy")
	}
	current, _ := store.Limit("research")
	if current.AmountCents != 100 {
		t.Fatal("denied effect published new policy")
	}
}
