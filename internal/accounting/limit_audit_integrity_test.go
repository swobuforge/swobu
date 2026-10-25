package accounting

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLimitAuditReopenRejectsCorruptOrDuplicateReceiptsWithoutRewrite(t *testing.T) {
	limit := Limit{Currency: "USD", AmountCents: 100, Window: "day", Mode: ModeStopNewBillableWork, UnknownPrice: "reject"}
	valid := LimitChange{ApprovalID: "approval", Workspace: "research", Actor: "local", Digest: "digest", Revision: "revision", After: limit, AppliedAt: time.Now().UTC()}
	for _, corruption := range []string{"approval", "workspace", "actor", "foreign_actor", "digest", "revision", "time", "after", "before", "duplicate"} {
		t.Run(corruption, func(t *testing.T) {
			change := valid
			switch corruption {
			case "approval":
				change.ApprovalID = ""
			case "workspace":
				change.Workspace = ""
			case "actor":
				change.Actor = ""
			case "foreign_actor":
				change.Actor = "model"
			case "digest":
				change.Digest = ""
			case "revision":
				change.Revision = ""
			case "time":
				change.AppliedAt = time.Time{}
			case "after":
				change.After.AmountCents = 0
			case "before":
				invalid := limit
				invalid.Currency = "unknown"
				change.Before = &invalid
			}
			changes := []LimitChange{change}
			if corruption == "duplicate" {
				changes = append(changes, change)
			}
			body, err := json.Marshal(persisted{Limits: map[string]Limit{"research": limit}, LimitChanges: changes})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "accounting.json")
			if err := os.WriteFile(path, body, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(path); err == nil {
				t.Fatal("corrupt audit receipt accepted")
			}
			retained, err := os.ReadFile(path)
			if err != nil || string(retained) != string(body) {
				t.Fatal("rejected receipt rewrote authoritative bytes")
			}
		})
	}
}

func TestLimitAuditReopenPreservesHistoricalBeforeAfterWithoutInventingCoverage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounting.json")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	limit := Limit{Currency: "USD", AmountCents: 100, Window: "day", Mode: ModeStopNewBillableWork, UnknownPrice: "reject"}
	change := LimitChange{ApprovalID: "approval", Workspace: "research", Actor: "local", Digest: "digest", Revision: store.Revision()}
	if err := store.ApplyReviewedLimitChecked(t.Context(), "research", limit, change, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	changed := limit
	changed.AmountCents = 200
	change.ApprovalID, change.Revision = "second", store.Revision()
	if err := store.ApplyReviewedLimitChecked(t.Context(), "research", changed, change, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	direct := limit
	direct.AmountCents = 300
	if err := store.SetLimit("research", direct); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	receipts := reopened.LimitChanges("research")
	if len(receipts) != 2 || receipts[0].Before != nil || receipts[0].After != limit || receipts[1].Before == nil || *receipts[1].Before != limit || receipts[1].After != changed {
		t.Fatal("historical audit was rewritten to current configuration")
	}
	current, found := reopened.Limit("research")
	if !found || current != direct {
		t.Fatal("historical audit replaced current limit")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(body) {
		t.Fatal("valid audit reopen rewrote bytes")
	}
}

func TestApprovedLimitReplayCannotChangeActor(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "accounting.json"))
	if err != nil {
		t.Fatal(err)
	}
	limit := Limit{Currency: "USD", AmountCents: 100, Window: "day", Mode: ModeStopNewBillableWork, UnknownPrice: "reject"}
	change := LimitChange{ApprovalID: "approval", Workspace: "research", Actor: "local", Digest: "digest", Revision: store.Revision()}
	if err := store.ApplyReviewedLimitChecked(t.Context(), "research", limit, change, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	change.Actor = "different_actor"
	if err := store.ApplyReviewedLimitChecked(t.Context(), "research", limit, change, func(context.Context) error { return nil }); err == nil {
		t.Fatal("replay changed authoritative actor binding")
	}
}
