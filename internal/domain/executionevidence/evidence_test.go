package executionevidence

import (
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/execution"
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
