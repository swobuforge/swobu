package accounting

import (
	"math/big"
	"time"
)

// Status is a read-only synchronous accounting projection. Decimal-string
// totals preserve cents beyond int64 and browser numeric precision. Charged
// includes conservative uncertain estimates, not confirmed provider invoices.
type Status struct {
	Workspace                 string    `json:"workspace"`
	Limit                     *Limit    `json:"limit"`
	WindowKey                 string    `json:"window_key"`
	WindowStart               time.Time `json:"window_start"`
	WindowEnd                 time.Time `json:"window_end"`
	Currency                  string    `json:"currency"`
	ReservedCents             string    `json:"reserved_cents"`
	SettledCents              string    `json:"settled_cents"`
	UncertainCents            string    `json:"uncertain_cents"`
	ChargedCents              string    `json:"charged_cents"`
	RemainingCents            *string   `json:"remaining_cents"`
	UnknownPriceOperations    int       `json:"unknown_price_operations"`
	PartialQuantityOperations int       `json:"partial_quantity_operations"`
	UnquotedOperations        int       `json:"unquoted_operations"`
}

func (store *Store) Status(workspace string) Status {
	status, _ := store.StatusWithRevision(workspace)
	return status
}

// StatusWithRevision binds the displayed accounting evidence and its strong
// management precondition to one locked snapshot. A separate Revision() call
// could race a policy change and authorize clearing a different policy.
func (store *Store) StatusWithRevision(workspace string) (Status, string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.statusLocked(workspace), store.revisionLocked()
}

func (store *Store) statusLocked(workspace string) Status {
	limit, limited := store.state.Limits[workspace]
	day := store.now().UTC().Format("2006-01-02")
	if store.state.WindowFloor > day {
		day = store.state.WindowFloor
	}
	start, _ := time.Parse("2006-01-02", day)
	key, end := day, start.AddDate(0, 0, 1)
	if limit.Window == "month" {
		key = day[:7]
		start = time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
		end = start.AddDate(0, 1, 0)
	}
	status := Status{Workspace: workspace, WindowKey: key, WindowStart: start, WindowEnd: end, Currency: "USD"}
	if limited {
		status.Limit = &limit
	}
	reserved, settled, uncertain, total := new(big.Int), new(big.Int), new(big.Int), new(big.Int)
	for _, reservation := range store.state.Reservations {
		if reservation.Workspace != workspace || reservation.WindowKey != key || reservation.State == "released" {
			continue
		}
		amount := big.NewInt(reservation.AmountCents)
		switch reservation.State {
		case "reserved":
			reserved.Add(reserved, amount)
		case "settled":
			settled.Add(settled, amount)
		case "uncertain":
			uncertain.Add(uncertain, amount)
		}
		total.Add(total, amount)
		if reservation.Coverage != CoverageKnown {
			status.UnknownPriceOperations++
		}
		if reservation.Quote == nil {
			status.UnquotedOperations++
		} else if reservation.Quote.QuantityCoverage == "partial" {
			status.PartialQuantityOperations++
		}
	}
	status.ReservedCents, status.SettledCents, status.UncertainCents, status.ChargedCents = reserved.String(), settled.String(), uncertain.String(), total.String()
	if limited {
		remaining := new(big.Int).Sub(big.NewInt(limit.AmountCents), total)
		if remaining.Sign() < 0 {
			remaining.SetInt64(0)
		}
		value := remaining.String()
		status.RemainingCents = &value
	}
	return status
}
