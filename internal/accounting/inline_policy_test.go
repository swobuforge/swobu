package accounting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInlinePolicyIsNotRuntimeEvidence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "accounting.json")
	var current []byte
	publisher := func(raw []byte) error { current = append([]byte(nil), raw...); return nil }
	s, err := OpenPolicyInline(path, nil, publisher)
	if err != nil {
		t.Fatal(err)
	}
	limit := Limit{Currency: "USD", AmountCents: 200, Window: "day", Mode: ModeStopNewBillableWork, UnknownPrice: "reject"}
	if err := s.SetLimit("research", limit); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("policy write created a runtime ledger")
	}
	if !bytes.Contains(current, []byte(`"amount_cents":200`)) {
		t.Fatalf("missing policy: %s", current)
	}
	if bytes.Contains(current, []byte(`"reservations"`)) {
		t.Fatal("execution evidence in Node policy")
	}
	reservation, err := s.Reserve("research", "exchange-1", 100, true)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"limits"`)) || bytes.Contains(raw, []byte(`"limit_changes"`)) {
		t.Fatalf("desired policy leaked to runtime ledger: %s", raw)
	}
	if !bytes.Contains(raw, []byte(`"reserved"`)) {
		t.Fatal("missing durable reservation")
	}
	reopened, err := OpenPolicyInline(path, current, publisher)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := reopened.Limit("research"); !ok || got != limit {
		t.Fatalf("restarted policy=%#v, %v", got, ok)
	}
	if got := reopened.Reservations("research"); len(got) != 1 || got[0].Operation != reservation.Operation {
		t.Fatalf("restarted reservation=%#v", got)
	}
	if err := reopened.SettleReservation("research", reservation.WindowKey, reservation.Operation, 125, false); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(after, []byte(`"settled"`)) {
		t.Fatal("settlement not durable")
	}
	if !bytes.Equal(current, []byte(`{"schema_version":1,"limits":{"research":{"currency":"USD","amount_cents":200,"window":"day","mode":"stop_new_billable_work","unknown_price":"reject"}}}`)) {
		// Only verify that settlement does not rewrite policy, regardless of JSON key ordering.
		var doc map[string]any
		if err := json.Unmarshal(current, &doc); err != nil {
			t.Fatal(err)
		}
		if _, ok := doc["reservations"]; ok {
			t.Fatal("reservation leaked into policy")
		}
	}
}

func TestInlinePolicyApprovedReplayAndFailureAtomicity(t *testing.T) {
	var current []byte
	failure := errors.New("writer unavailable")
	fail := false
	publisher := func(raw []byte) error {
		if fail {
			return failure
		}
		current = append([]byte(nil), raw...)
		return nil
	}
	s, err := OpenPolicyInline(filepath.Join(t.TempDir(), "accounting.json"), nil, publisher)
	if err != nil {
		t.Fatal(err)
	}
	limit := Limit{Currency: "USD", AmountCents: 300, Window: "month", Mode: ModeStopNewBillableWork, UnknownPrice: "reject"}
	rev := s.Revision()
	approve := LimitChange{ApprovalID: "abc", Workspace: "research", Actor: "local", Digest: "hash", Revision: rev}
	fail = true
	if err := s.ApplyReviewedLimitChecked(t.Context(), "research", limit, approve, func(context.Context) error { return nil }); !errors.Is(err, failure) {
		t.Fatalf("error=%v", err)
	}
	if _, ok := s.Limit("research"); ok || len(current) > 0 || len(s.AllLimitChanges()) > 0 || s.Revision() != rev {
		t.Fatal("failed policy commit became observable")
	}
	fail = false
	if err := s.ApplyReviewedLimitChecked(t.Context(), "research", limit, approve, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyReviewedLimitChecked(t.Context(), "research", limit, approve, func(context.Context) error { return nil }); err != nil {
		t.Fatal("idempotent replay", err)
	}
	if len(s.AllLimitChanges()) != 1 {
		t.Fatal("duplicate approved receipts")
	}
	if err := s.SetLimitAtRevision("research", limit, rev); err == nil {
		t.Fatal("accepted stale policy revision")
	}
	if _, err := OpenPolicyInline(filepath.Join(t.TempDir(), "accounting.json"), current, publisher); err != nil {
		t.Fatal("approved policy failed to reopen", err)
	}
}

func TestInlinePolicyRejectsObsoleteOrMalformedRuntimeStateWithoutRewrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "accounting.json")
	examples := []string{`{"limits":{},"reservations":[]}`, `{"limit_changes":[]}`, `{"surprise":"yes"}`, `{"reservations":123}`}
	for _, bad := range examples {
		if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := OpenPolicyInline(path, nil, func([]byte) error { return nil })
		if err == nil {
			t.Fatalf("accepted obsolete or broken runtime: %s", bad)
		}
		after, _ := os.ReadFile(path)
		if string(after) != bad {
			t.Fatal("bad sidecar was rewritten")
		}
	}
}

func TestInlinePolicyValidationAndDetachedRevision(t *testing.T) {
	cases := []string{`{"schema_version":2,"limits":{}}`, `{"schema_version":1,"limits":null}`, `{"schema_version":1,"limits":{"research":{"currency":"EUR","amount_cents":1,"window":"day","mode":"stop_new_billable_work","unknown_price":"reject"}}}`, `{"schema_version":1,"limits":{},"unexpected":1}`, `{"schema_version":1,"limits":{}} {}`}
	for _, raw := range cases {
		if _, err := ValidatePolicyDocument([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	policy := `{"schema_version":1,"limits":{}}`
	if _, err := ValidatePolicyDocument([]byte(policy)); err != nil {
		t.Fatal(err)
	}
	var node []byte
	s, err := OpenPolicyInline(filepath.Join(t.TempDir(), "accounting.json"), []byte(policy), func(raw []byte) error { node = append([]byte(nil), raw...); return nil })
	if err != nil {
		t.Fatal(err)
	}
	before := s.Revision()
	if _, err := s.Reserve("research", "unlimited", 0, false); err != nil {
		t.Fatal(err)
	}
	if before != s.Revision() {
		t.Fatal("in-flight reservation invalidated policy approval")
	}
	if strings.Contains(string(node), "reservations") {
		t.Fatal("reservation rewrote policy")
	}
}

func TestClearInlinePolicyPreservesReservationsAndRecoversOnWriteFailure(t *testing.T) {
	failure := false
	var saved []byte
	s, err := OpenPolicyInline(filepath.Join(t.TempDir(), "accounting.json"), nil, func(raw []byte) error {
		if failure {
			return errors.New("storage failure")
		}
		saved = append([]byte(nil), raw...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	limit := Limit{Currency: "USD", AmountCents: 100, Window: "day", Mode: ModeStopNewBillableWork, UnknownPrice: "reject"}
	if err := s.SetLimit("research", limit); err != nil {
		t.Fatal(err)
	}
	reservation, err := s.Reserve("research", "request", 80, true)
	if err != nil {
		t.Fatal(err)
	}
	revision := s.Revision()
	failure = true
	if err := s.ClearLimitAtRevision("research", revision); err == nil {
		t.Fatal("accepted failed policy delete")
	}
	if got, ok := s.Limit("research"); !ok || got != limit || s.Revision() != revision {
		t.Fatal("failed delete dropped limit")
	}
	failure = false
	if err := s.ClearLimitAtRevision("research", revision); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Limit("research"); ok {
		t.Fatal("limit not cleared")
	}
	if got := s.Reservations("research"); len(got) != 1 || got[0].Operation != reservation.Operation {
		t.Fatal("reservation lost")
	}
	if err := s.ClearLimitAtRevision("research", revision); err == nil {
		t.Fatal("stale revision accepted")
	}
	if _, err := ValidatePolicyDocument(saved); err != nil {
		t.Fatal("cleared policy invalid", err)
	}
}
