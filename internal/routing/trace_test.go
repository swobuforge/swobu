package routing

import (
	"testing"
	"time"
)

func TestTrace_RecordRouteResolved(t *testing.T) {
	tr := &Trace{}
	tr.RecordRouteResolved("free")
	if len(tr.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(tr.Events))
	}
	if tr.Events[0].Kind != TraceRouteResolved {
		t.Errorf("Kind = %v, want %v", tr.Events[0].Kind, TraceRouteResolved)
	}
	if tr.Events[0].Detail != "free" {
		t.Errorf("Detail = %q, want free", tr.Events[0].Detail)
	}
}

func TestTrace_RecordTargetFiltered(t *testing.T) {
	tr := &Trace{}
	tr.RecordTargetFiltered("ollama-qwen", FilterContextTooSmall, "estimated 8k > 4k")
	if len(tr.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(tr.Events))
	}
	if tr.Events[0].Kind != TraceTargetFiltered {
		t.Errorf("Kind = %v", tr.Events[0].Kind)
	}
	if tr.Events[0].TargetID != "ollama-qwen" {
		t.Errorf("TargetID = %q", tr.Events[0].TargetID)
	}
	if tr.Events[0].Reason != string(FilterContextTooSmall) {
		t.Errorf("Reason = %q", tr.Events[0].Reason)
	}
}

func TestTrace_RecordPlanBuilt(t *testing.T) {
	tr := &Trace{}
	tr.RecordPlanBuilt("r1 A → r2 B")
	if len(tr.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(tr.Events))
	}
	if tr.Events[0].Kind != TracePlanBuilt {
		t.Errorf("Kind = %v", tr.Events[0].Kind)
	}
	if tr.Events[0].Detail != "r1 A → r2 B" {
		t.Errorf("Detail = %q", tr.Events[0].Detail)
	}
}

func TestTrace_RecordAttempt(t *testing.T) {
	tr := &Trace{}
	tr.RecordAttempt("chatgpt", 1)
	if len(tr.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(tr.Events))
	}
	if tr.Events[0].Kind != TraceAttempt {
		t.Errorf("Kind = %v", tr.Events[0].Kind)
	}
	if tr.Events[0].TargetID != "chatgpt" {
		t.Errorf("TargetID = %q", tr.Events[0].TargetID)
	}
	if tr.Events[0].Rank != 1 {
		t.Errorf("Rank = %d, want 1", tr.Events[0].Rank)
	}
}

func TestTrace_RecordFailure_BeforeStream(t *testing.T) {
	tr := &Trace{}
	tr.RecordFailure("chatgpt", FailureRateLimited, false)
	if len(tr.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(tr.Events))
	}
	if tr.Events[0].Kind != TraceFailure {
		t.Errorf("Kind = %v", tr.Events[0].Kind)
	}
	if tr.Events[0].Reason != string(FailureRateLimited) {
		t.Errorf("Reason = %q", tr.Events[0].Reason)
	}
	if tr.Events[0].Detail != "rate_limited before stream" {
		t.Errorf("Detail = %q", tr.Events[0].Detail)
	}
}

func TestTrace_RecordFailure_AfterStream(t *testing.T) {
	tr := &Trace{}
	tr.RecordFailure("chatgpt", FailureServerError, true)
	if len(tr.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(tr.Events))
	}
	if tr.Events[0].Detail != "server_error after stream started" {
		t.Errorf("Detail = %q", tr.Events[0].Detail)
	}
}

func TestTrace_RecordCooldown(t *testing.T) {
	tr := &Trace{}
	tr.RecordCooldown("chatgpt", FailureRateLimited, 60*time.Second)
	if len(tr.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(tr.Events))
	}
	if tr.Events[0].Kind != TraceCooldown {
		t.Errorf("Kind = %v", tr.Events[0].Kind)
	}
	if tr.Events[0].TargetID != "chatgpt" {
		t.Errorf("TargetID = %q", tr.Events[0].TargetID)
	}
	if tr.Events[0].Detail != "1m0s" {
		t.Errorf("Detail = %q, want 1m0s", tr.Events[0].Detail)
	}
}

func TestTrace_RecordSuccess(t *testing.T) {
	tr := &Trace{}
	tr.RecordSuccess("chatgpt")
	if len(tr.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(tr.Events))
	}
	if tr.Events[0].Kind != TraceSuccess {
		t.Errorf("Kind = %v", tr.Events[0].Kind)
	}
	if tr.Events[0].TargetID != "chatgpt" {
		t.Errorf("TargetID = %q", tr.Events[0].TargetID)
	}
}

func TestTrace_RecordFinalFailure(t *testing.T) {
	tr := &Trace{}
	tr.RecordFinalFailure("no_target_succeeded")
	if len(tr.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(tr.Events))
	}
	if tr.Events[0].Kind != TraceFinalFailure {
		t.Errorf("Kind = %v", tr.Events[0].Kind)
	}
	if tr.Events[0].Detail != "no_target_succeeded" {
		t.Errorf("Detail = %q", tr.Events[0].Detail)
	}
}

func TestTrace_MultipleEvents(t *testing.T) {
	tr := &Trace{}
	tr.RecordRouteResolved("free")
	tr.RecordTargetFiltered("ollama", FilterToolsUnsupported, "")
	tr.RecordPlanBuilt("r1 A")
	tr.RecordAttempt("A", 1)
	tr.RecordSuccess("A")

	if len(tr.Events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(tr.Events))
	}
	expected := []TraceKind{TraceRouteResolved, TraceTargetFiltered, TracePlanBuilt, TraceAttempt, TraceSuccess}
	for i, want := range expected {
		if tr.Events[i].Kind != want {
			t.Errorf("event[%d].Kind = %v, want %v", i, tr.Events[i].Kind, want)
		}
	}
	// Events should have monotonically non-decreasing time.
	for i := 1; i < len(tr.Events); i++ {
		if tr.Events[i].Time.Before(tr.Events[i-1].Time) {
			t.Errorf("event[%d].Time %v before event[%d].Time %v", i, tr.Events[i].Time, i-1, tr.Events[i-1].Time)
		}
	}
}
