package accounting

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAllLimitChangesRetainsDetachedFactsAcrossWorkspacesAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounting.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	limit := Limit{Currency: "USD", AmountCents: 100, Window: "day", Mode: ModeStopNewBillableWork, UnknownPrice: "reject"}
	for _, workspace := range []string{"research", "coding"} {
		for _, approval := range []string{"first", "second"} {
			if err := store.ApplyReviewedLimitChecked(t.Context(), workspace, limit, LimitChange{ApprovalID: workspace + approval, Workspace: workspace, Actor: "local", Digest: "digest", Revision: store.Revision()}, func(context.Context) error { return nil }); err != nil {
				t.Fatal(err)
			}
			limit.AmountCents++
		}
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	changes := store.AllLimitChanges()
	if len(changes) != 4 || len(store.LimitChanges("research")) != 2 || len(store.LimitChanges("coding")) != 2 {
		t.Fatal("Node audit omitted another Workspace")
	}
	if changes[1].Before == nil {
		t.Fatal("original before missing")
	}
	original := *changes[1].Before
	changes[1].Before.AmountCents = 999
	changes[0].After.AmountCents = 999
	fresh := store.AllLimitChanges()
	if *fresh[1].Before != original || fresh[0].After.AmountCents != 100 {
		t.Fatal("Node audit aliases authoritative receipts")
	}
}
