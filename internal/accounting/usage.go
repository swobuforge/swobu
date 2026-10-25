package accounting

import (
	"math/big"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// ObservedUSDCents uses the frozen price vector, not a newly acquired tariff.
// Canonical cache/reasoning quantities are subsets of input/output totals.
// Every dimension must be known; missing rates for nonzero dimensions remain
// unknown rather than inheriting an ordinary rate without provider authority.
// Observed input must match the quoted band-selection quantity: the pinned
// snapshot does not contain other bands and cannot prove a changed input price.
func (quote Quote) ObservedUSDCents(usage canonical.TokenUsage) (int64, bool) {
	if quote.Validate() != nil {
		return 0, false
	}
	input, inputKnown := usage.InputTokens()
	output, outputKnown := usage.OutputTokens()
	read, readKnown := usage.CacheReadTokens()
	write, writeKnown := usage.CacheWriteTokens()
	reasoning, reasoningKnown := usage.ReasoningTokens()
	if !inputKnown || !outputKnown || !readKnown || !writeKnown || !reasoningKnown || read > input || write > input-read || reasoning > output || int64(input) != quote.InputTokens {
		return 0, false
	}
	total := new(big.Rat)
	components := []struct {
		tokens int
		rate   *string
	}{
		{input - read - write, &quote.Price.InputPerMillion},
		{output - reasoning, &quote.Price.OutputPerMillion},
		{read, quote.Price.CacheReadPerMillion},
		{write, quote.Price.CacheWritePerMillion},
		{reasoning, quote.Price.ReasoningPerMillion},
	}
	for _, component := range components {
		if component.tokens == 0 {
			continue
		}
		if component.rate == nil {
			return 0, false
		}
		rate, ok := new(big.Rat).SetString(*component.rate)
		if !ok {
			return 0, false
		}
		total.Add(total, new(big.Rat).Mul(rate, new(big.Rat).SetInt64(int64(component.tokens))))
	}
	total.Quo(total, big.NewRat(10000, 1))
	cents, remainder := new(big.Int), new(big.Int)
	cents.QuoRem(total.Num(), total.Denom(), remainder)
	if remainder.Sign() != 0 {
		cents.Add(cents, big.NewInt(1))
	}
	if !cents.IsInt64() {
		return 0, false
	}
	return cents.Int64(), true
}

// SettleUsage updates the exact admitted window. Incomplete usage keeps the
// conservative admitted estimate uncertain; it never turns absent usage into $0.
func (store *Store) SettleUsage(workspace, window, operation string, usage canonical.TokenUsage, completed bool) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, reservation := range store.state.Reservations {
		if reservation.Workspace != workspace || reservation.WindowKey != window || reservation.Operation != operation {
			continue
		}
		amount, known := reservation.AmountCents, false
		if completed && reservation.Quote != nil {
			amount, known = reservation.Quote.ObservedUSDCents(usage)
			if !known {
				amount = reservation.AmountCents
			}
		}
		return store.settleLocked(workspace, window, operation, amount, !completed || !known)
	}
	return store.settleLocked(workspace, window, operation, 0, true)
}
