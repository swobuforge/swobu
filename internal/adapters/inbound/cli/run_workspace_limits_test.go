package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/accounting"
)

func TestWorkspaceLimitsPreservesExactOwnerStatusWithoutMutation(t *testing.T) {
	for _, mode := range []string{"human", "json", "unconfigured", "observe-only", "wrong-workspace", "unavailable", "missing-workspace", "extra-argument", "help"} {
		t.Run(mode, func(t *testing.T) {
			remaining := "0"
			status := accounting.Status{
				Workspace: "coding", Currency: "USD", WindowKey: "2026-10-13",
				WindowStart: time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC), WindowEnd: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC),
				Limit:        &accounting.Limit{Currency: "USD", AmountCents: 100, Window: "day", Mode: accounting.ModeStopNewBillableWork, UnknownPrice: "reject"},
				ChargedCents: "18446744073709551614", ReservedCents: "9223372036854775807", UncertainCents: "9223372036854775807", SettledCents: "0", RemainingCents: &remaining,
				UnknownPriceOperations: 2, PartialQuantityOperations: 3, UnquotedOperations: 4,
			}
			if mode == "unconfigured" {
				status.Limit, status.RemainingCents = nil, nil
			}
			if mode == "observe-only" {
				status.Limit.Mode = accounting.ModeObserveOnly
			}
			if mode == "wrong-workspace" {
				status.Workspace = "research"
			}
			var calls, reads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				calls.Add(1)
				if request.Method != http.MethodGet || request.URL.RawQuery != "" || request.Header.Get("Authorization") != "" {
					t.Error("status command issued a mutation, query or credential")
				}
				if request.URL.Path == "/api/v1/status" {
					fmt.Fprint(response, `{"state":"healthy","control_plane_protocol":11}`)
					return
				}
				reads.Add(1)
				if request.URL.Path != "/api/v1/workspaces/coding/limits/status" {
					t.Error("noncanonical status path")
				}
				if mode == "unavailable" {
					http.Error(response, "unavailable", http.StatusServiceUnavailable)
					return
				}
				if err := json.NewEncoder(response).Encode(status); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			args := []string{"workspace", "limits", "--workspace", "coding", "--addr", strings.TrimPrefix(server.URL, "http://")}
			if mode == "json" {
				args = append(args, "--json")
			}
			if mode == "missing-workspace" {
				args = []string{"workspace", "limits"}
			}
			if mode == "extra-argument" {
				args = append(args, "surprise")
			}
			if mode == "help" {
				args = []string{"workspace", "limits", "--help"}
			}
			var stdout, stderr bytes.Buffer
			runner := Runner{Stdout: &stdout, Stderr: &stderr, IsInteractive: func() bool { return false }}
			exit := runner.Run(t.Context(), args)
			if mode == "missing-workspace" || mode == "extra-argument" || mode == "help" {
				if calls.Load() != 0 || stdout.Len() != 0 || (mode == "help" && exit != ExitHealthy) || (mode != "help" && exit != ExitDown) {
					t.Fatalf("local argument handling exit=%d calls=%d output=%q", exit, calls.Load(), stdout.String())
				}
				return
			}
			if reads.Load() != 1 {
				t.Fatalf("status reads=%d", reads.Load())
			}
			if mode == "wrong-workspace" || mode == "unavailable" {
				if exit != ExitDown || stdout.Len() != 0 || stderr.Len() == 0 {
					t.Fatal("failed read fabricated status")
				}
				return
			}
			if exit != ExitHealthy || stderr.Len() != 0 {
				t.Fatalf("status failed: %d %s", exit, stderr.String())
			}
			if mode == "json" {
				var decoded accounting.Status
				if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil || !reflect.DeepEqual(status, decoded) {
					t.Fatalf("JSON changed exact owner status: %v %#v", err, decoded)
				}
				return
			}
			for _, text := range []string{"18446744073709551614", "9223372036854775807", "2 unknown-price, 3 partial-quantity, 4 unquoted", "not provider invoices", "2026-10-14T00:00:00Z", "unmeasured cost"} {
				if !strings.Contains(stdout.String(), text) {
					t.Fatalf("human status missing %q: %s", text, stdout.String())
				}
			}
			if mode == "unconfigured" && (!strings.Contains(stdout.String(), "not configured") || strings.Contains(stdout.String(), "Remaining")) {
				t.Fatal("unconfigured limit fabricated remaining capacity")
			}
			if mode == "observe-only" && !strings.Contains(stdout.String(), "mode=observe_only") {
				t.Fatal("observation misrepresented enforcement")
			}
		})
	}
}
