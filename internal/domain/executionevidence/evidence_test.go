package executionevidence

import (
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/clientfamily"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/execution"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestManagedAdmissionRejectionIsCoarseAndBounded(t *testing.T) {
	workspace, err := routing.ParseWorkspaceSlug("alpha")
	if err != nil {
		t.Fatal(err)
	}
	managed := accessidentity.NewManaged([16]byte{1})
	cases := []struct {
		name   string
		access accessidentity.ID
		reason AdmissionReason
		wantID bool
	}{
		{"invalid credential", accessidentity.ID{}, InvalidCredential, false},
		{"ambiguous credential", accessidentity.ID{}, AmbiguousCredentials, false},
		{"workspace mismatch", managed, WorkspaceMismatch, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rejection, err := NewManagedAccessAdmissionRejected(time.Now(), workspace, tc.access, tc.reason)
			if err != nil {
				t.Fatal(err)
			}
			if rejection.Reason() != tc.reason || rejection.Workspace() != workspace {
				t.Fatalf("rejection = %#v", rejection)
			}
			if rejection.AccessID().Valid() != tc.wantID {
				t.Fatalf("access identity visibility = %v, want %v", rejection.AccessID().Valid(), tc.wantID)
			}
		})
	}
	if _, err := NewManagedAccessAdmissionRejected(time.Now(), workspace, accessidentity.Local, WorkspaceMismatch); err == nil {
		t.Fatal("workspace mismatch accepted a non-managed access identity")
	}
}

func TestExchangeSettlementRejectsOpenErrorCode(t *testing.T) {
	id, err := execution.NewExchangeID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewExchangeSettled(id, time.Now(), delivery.ExchangeFailed, 500, canonical.ErrorCode("provider-prose")); err == nil {
		t.Fatal("exchange settlement accepted a non-canonical error code")
	}
}

func TestMCPInvocationCancellationNeedsNoFailureSubtype(t *testing.T) {
	exchangeID, err := execution.NewExchangeID()
	if err != nil {
		t.Fatal(err)
	}
	id, err := execution.NewInvocationID(exchangeID, 1)
	if err != nil {
		t.Fatal(err)
	}
	settled, err := NewMCPInvocationSettled(id, time.Now(), execution.Cancelled, execution.MayHaveOccurred, "")
	if err != nil || settled.Failure() != "" {
		t.Fatalf("cancelled MCP with no failure: settlement=%#v err=%v", settled, err)
	}
	if _, err := NewMCPInvocationSettled(id, time.Now(), execution.Cancelled, execution.MayHaveOccurred, FailureMCPTransport); err == nil {
		t.Fatal("cancelled MCP accepted duplicate failure category")
	}
}

func TestModelResourceFreezesConnectionRevision(t *testing.T) {
	route, _ := routing.ParseRouteName("primary")
	target, _ := routing.ParseTargetID("target")
	model, _ := routing.ParseUpstreamModel("model")
	original, err := NewModelResource(route, target, 7, "openai", protocolkind.Responses, model, ExchangeAttempt)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := connectionidentity.Parse("cn_account")
	bound, err := original.BindConnection(id, 2)
	if err != nil {
		t.Fatal(err)
	}
	gotID, revision, ok := bound.ConnectionIdentity()
	if !ok || gotID != id || revision != 2 || bound.Version() != 7 {
		t.Fatal("composite identity lost")
	}
	if _, _, ok := original.ConnectionIdentity(); ok {
		t.Fatal("binding mutated prior value")
	}
	if _, err := bound.BindConnection(id, 3); err == nil {
		t.Fatal("rebound retained identity")
	}
	if _, err := original.BindConnection(id, 0); err == nil {
		t.Fatal("accepted zero revision")
	}
	if _, err := original.BindConnection(connectionidentity.ID{}, 2); err == nil {
		t.Fatal("accepted absent Connection")
	}
}

// Equal observed instants are possible on real clocks and after persistence
// strips Go's monotonic component. Event arrival and identity prove causality.
func TestZeroResolutionExchangeAndInvocations(t *testing.T) {
	at := time.Date(2026, 10, 21, 17, 5, 6, 123000000, time.UTC)
	exchangeID, err := execution.NewExchangeID()
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := routing.ParseWorkspaceSlug("coding")
	start, err := NewExchangeStarted(exchangeID, accessidentity.Local, workspace, at, clientfamily.Codex, protocolkind.Responses, canonical.NormalizedPathResponses)
	if err != nil {
		t.Fatal(err)
	}
	settle, err := NewExchangeSettled(exchangeID, at, delivery.Succeeded, 200, "")
	if err != nil {
		t.Fatal(err)
	}

	mcpID, _ := execution.NewInvocationID(exchangeID, 1)
	tool, _ := canonical.NewToolKey("mcp", canonical.ToolKindMCP, "search")
	mcpResource, _ := NewMCPResource(tool, tool)
	mcpStart, err := NewMCPInvocationStarted(mcpID, at, mcpResource)
	if err != nil {
		t.Fatal(err)
	}
	mcpSettle, err := NewMCPInvocationSettled(mcpID, at, execution.Succeeded, execution.Occurred, "")
	if err != nil {
		t.Fatal(err)
	}

	modelID, _ := execution.NewInvocationID(exchangeID, 2)
	route, _ := routing.ParseRouteName("coding")
	target, _ := routing.ParseTargetID("target")
	model, _ := routing.ParseUpstreamModel("model")
	modelResource, err := NewModelResource(route, target, 1, "openai", protocolkind.Responses, model, ExchangeAttempt)
	if err != nil {
		t.Fatal(err)
	}
	modelStart, err := NewModelInvocationStarted(modelID, at, modelResource)
	if err != nil {
		t.Fatal(err)
	}
	modelSettle, err := NewModelInvocationSettled(modelID, at, execution.Succeeded, execution.Occurred, "", canonical.NewUnknownTokenUsage())
	if err != nil {
		t.Fatal(err)
	}

	invocations := make([]InvocationLifecycle, 0, 2)
	for _, facts := range []struct {
		name    string
		started InvocationStartedEvent
		settled InvocationSettledEvent
	}{
		{"mcp", mcpStart, mcpSettle},
		{"model", modelStart, modelSettle},
	} {
		t.Run(facts.name, func(t *testing.T) {
			live, err := NewInvocationLifecycle(facts.started, facts.settled)
			if err != nil || live.Duration() != 0 {
				t.Fatalf("equal-clock invocation = %#v, %v", live, err)
			}
			invocations = append(invocations, live)
			restored, err := RestoreInvocationLifecycle(facts.started, facts.settled, 50*time.Microsecond)
			if err != nil || restored.Duration() != 50*time.Microsecond {
				t.Fatalf("positive measured duration lost across equal-clock timestamps: %#v, %v", restored, err)
			}
			if _, err := RestoreInvocationLifecycle(facts.started, facts.settled, -time.Nanosecond); err == nil {
				t.Fatal("negative duration accepted")
			}
		})
	}
	completed, err := NewCompletedExchange(start, invocations, settle)
	if err != nil || completed.Duration() != 0 || len(completed.Invocations()) != 2 {
		t.Fatalf("equal-clock Exchange = %#v, %v", completed, err)
	}
	restored, err := RestoreCompletedExchange(start, invocations, settle, time.Microsecond)
	if err != nil || restored.Duration() != time.Microsecond {
		t.Fatalf("recorded elapsed time changed: %#v, %v", restored, err)
	}
	if _, err := RestoreCompletedExchange(start, invocations, settle, -time.Nanosecond); err == nil {
		t.Fatal("negative Exchange duration accepted")
	}

	before := at.Add(-time.Nanosecond)
	backdatedMCP, _ := NewMCPInvocationSettled(mcpID, before, execution.Succeeded, execution.Occurred, "")
	if _, err := NewInvocationLifecycle(mcpStart, backdatedMCP); err == nil {
		t.Fatal("backdated MCP settlement accepted")
	}
	backdatedModel, _ := NewModelInvocationSettled(modelID, before, execution.Succeeded, execution.Occurred, "", canonical.NewUnknownTokenUsage())
	if _, err := NewInvocationLifecycle(modelStart, backdatedModel); err == nil {
		t.Fatal("backdated model settlement accepted")
	}
	backdatedExchange, _ := NewExchangeSettled(exchangeID, before, delivery.Succeeded, 200, "")
	if _, err := NewCompletedExchange(start, invocations, backdatedExchange); err == nil {
		t.Fatal("backdated Exchange settlement accepted")
	}

	otherExchange, _ := execution.NewExchangeID()
	otherInvocation, _ := execution.NewInvocationID(otherExchange, 1)
	wrongTerminal, _ := NewMCPInvocationSettled(otherInvocation, at, execution.Succeeded, execution.Occurred, "")
	if _, err := NewInvocationLifecycle(mcpStart, wrongTerminal); err == nil {
		t.Fatal("mismatched invocation identity accepted")
	}
	wrongExchange, _ := NewExchangeSettled(otherExchange, at, delivery.Succeeded, 200, "")
	if _, err := NewCompletedExchange(start, invocations, wrongExchange); err == nil {
		t.Fatal("mismatched Exchange identity accepted")
	}
	if _, err := NewInvocationLifecycle(MCPInvocationStarted{}, MCPInvocationSettled{}); err == nil {
		t.Fatal("zero-value invocation accepted")
	}
	if _, err := NewCompletedExchange(ExchangeStarted{}, nil, ExchangeSettled{}); err == nil {
		t.Fatal("zero-value Exchange accepted")
	}
}
