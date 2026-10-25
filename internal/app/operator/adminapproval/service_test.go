package adminapproval

import (
	"errors"
	"testing"
	"time"
)

func TestApprovalBindsDigestRevisionAndIsSingleUse(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc := New(func() time.Time { return now })
	approval, err := svc.Create("research", "inspect", "rev-4", map[string]any{"query": "x"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Consume(approval.ID, "research", "inspect", "rev-4", map[string]any{"query": "y"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err=%v", err)
	}
	if err := svc.Consume(approval.ID, "research", "inspect", "rev-4", map[string]any{"query": "x"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Consume(approval.ID, "research", "inspect", "rev-4", map[string]any{"query": "x"}); !errors.Is(err, ErrUsed) {
		t.Fatalf("err=%v", err)
	}
}
