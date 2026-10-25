package accounting

import (
	"path/filepath"
	"testing"
)

func TestStatusWithRevisionBindsLimitPolicyToCurrentRevision(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "accounting.json"))
	if err != nil {
		t.Fatal(err)
	}
	before, revision := store.StatusWithRevision("engineering")
	if before.Limit != nil || revision != store.Revision() {
		t.Fatalf("unexpected initial snapshot: %+v", before)
	}
	limit := Limit{Currency: "USD", AmountCents: 1750, Window: "day", Mode: ModeStopNewBillableWork, UnknownPrice: "reject"}
	if err := store.SetLimitAtRevision("engineering", limit, revision); err != nil {
		t.Fatal(err)
	}
	after, currentRevision := store.StatusWithRevision("engineering")
	if after.Limit == nil || *after.Limit != limit || currentRevision == revision || currentRevision != store.Revision() {
		t.Fatalf("status does not bind policy revision: %+v", after)
	}
	if err := store.ClearLimitAtRevision("engineering", revision); err == nil {
		t.Fatal("stale revision cleared a different policy")
	}
	if err := store.ClearLimitAtRevision("engineering", currentRevision); err != nil {
		t.Fatal(err)
	}
	cleared, _ := store.StatusWithRevision("engineering")
	if cleared.Limit != nil {
		t.Fatal("cleared policy remained")
	}
}
