package accounting

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestAccountingProcessCrashHelper(t *testing.T) {
	path := os.Getenv("SWOBU_ACCOUNTING_CRASH_TEST_PATH")
	if path == "" {
		return
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC) }
	if err := store.SetLimit("coding", Limit{Currency: "USD", AmountCents: 3, Window: "day", Mode: ModeStopNewBillableWork, UnknownPrice: "reject"}); err != nil {
		t.Fatal(err)
	}
	reservation, err := store.ReserveQuote("coding", "in-doubt", testQuote())
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("SWOBU_ACCOUNTING_CRASH_TEST_STAGE") == "uncertain" {
		if err := store.SettleReservation("coding", reservation.WindowKey, reservation.Operation, 3, true); err != nil {
			t.Fatal(err)
		}
	}
	fmt.Fprintln(os.Stdout, "ACCOUNTING_COMMITTED")
	input := make([]byte, 1)
	_, _ = os.Stdin.Read(input)
}

func TestAccountingAbruptProcessDeathPreservesQuoteAndEnforcement(t *testing.T) {
	for _, stage := range []string{"reserved", "uncertain"} {
		t.Run(stage, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "accounting.json")
			command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestAccountingProcessCrashHelper$", "-test.count=1")
			command.Env = append(os.Environ(), "SWOBU_ACCOUNTING_CRASH_TEST_PATH="+path, "SWOBU_ACCOUNTING_CRASH_TEST_STAGE="+stage)
			stdout, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			input, err := command.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if command.ProcessState == nil {
					_ = command.Process.Kill()
					_ = command.Wait()
				}
			})
			ready := make(chan bool, 1)
			go func() {
				scanner := bufio.NewScanner(stdout)
				for scanner.Scan() {
					if scanner.Text() == "ACCOUNTING_COMMITTED" {
						ready <- true
						return
					}
				}
				ready <- false
			}()
			select {
			case committed := <-ready:
				if !committed {
					t.Fatal("child exited before durable accounting acknowledgement")
				}
			case <-time.After(10 * time.Second):
				t.Fatal("child did not acknowledge durable accounting")
			}
			if err := command.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := command.Wait(); err == nil {
				t.Fatal("child completed normally rather than dying abruptly")
			}
			store, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			store.now = func() time.Time { return time.Date(2026, 10, 13, 11, 0, 0, 0, time.UTC) }
			reservations := store.Reservations("coding")
			quote := testQuote()
			if len(reservations) != 1 || reservations[0].State != stage || reservations[0].AmountCents != 3 || !reflect.DeepEqual(reservations[0].Quote, &quote) {
				t.Fatalf("process death changed quote/charge: %#v", reservations)
			}
			if _, err := store.Reserve("coding", "new-effect", 1, true); !errors.Is(err, ErrBudgetExceeded) {
				t.Fatalf("process death reset admission: %v", err)
			}
			changed := testQuote()
			changed.AmountCents = 2
			changed.Price.EffectiveAt = 200
			if _, err := store.ReserveQuote("coding", "in-doubt", changed); err != nil {
				t.Fatal(err)
			}
			if got := store.Reservations("coding"); !reflect.DeepEqual(got, reservations) {
				t.Fatal("post-crash replay repriced or duplicated the in-doubt attempt")
			}
			if err := store.SettleReservation("coding", reservations[0].WindowKey, "in-doubt", 3, true); err != nil {
				t.Fatal(err)
			}
			before := store.Revision()
			if err := store.SettleReservation("coding", reservations[0].WindowKey, "in-doubt", 3, true); err != nil || store.Revision() != before {
				t.Fatal("post-crash settlement replay changed authority")
			}
			status := store.Status("coding")
			if status.ChargedCents != "3" || status.UncertainCents != "3" || status.ReservedCents != "0" || status.PartialQuantityOperations != 1 {
				t.Fatalf("post-crash status lost conservative hold: %#v", status)
			}
			store.now = func() time.Time { return time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC) }
			if _, err := store.ReserveQuote("coding", "next-day", testQuote()); err != nil {
				t.Fatal(err)
			}
			if err := store.SettleReservation("coding", reservations[0].WindowKey, "in-doubt", 2, false); err != nil {
				t.Fatal(err)
			}
			if current := store.Status("coding"); current.ChargedCents != "3" || current.ReservedCents != "3" || current.SettledCents != "0" || current.WindowKey != "2026-10-14" {
				t.Fatalf("old-window post-crash settlement changed new-day capacity: %#v", current)
			}
		})
	}
}
