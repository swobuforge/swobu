package accounting

import (
	"errors"
	"regexp"
	"strings"
)

// PriceSnapshot is the economic identity frozen before one provider attempt.
// It records exact source rates, not credentials or a canonical Model guess.
type PriceSnapshot struct {
	Provider             string  `json:"provider"`
	ExternalID           string  `json:"external_id"`
	ExternalVersion      string  `json:"external_version"`
	Tariff               string  `json:"tariff"`
	EffectiveAt          int64   `json:"effective_at"`
	MinInputTokens       int64   `json:"min_input_tokens"`
	Currency             string  `json:"currency"`
	InputPerMillion      string  `json:"input_per_million"`
	OutputPerMillion     string  `json:"output_per_million"`
	CacheReadPerMillion  *string `json:"cache_read_per_million"`
	CacheWritePerMillion *string `json:"cache_write_per_million"`
	ReasoningPerMillion  *string `json:"reasoning_per_million"`
}

// Quote preserves the original reservation estimate independently of later
// settlement. Partial quantities mean an estimated threshold, never a hard cap.
type Quote struct {
	Price            PriceSnapshot `json:"price"`
	TargetID         string        `json:"target_id"`
	TargetVersion    uint64        `json:"target_version"`
	AmountCents      int64         `json:"amount_cents"`
	InputTokens      int64         `json:"input_tokens"`
	OutputTokens     int64         `json:"output_tokens"`
	QuantityCoverage string        `json:"quantity_coverage"`
}

var priceDecimal = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)

func (quote Quote) Validate() error {
	price := quote.Price
	if quote.TargetID == "" || quote.TargetVersion == 0 || quote.AmountCents < 0 || quote.InputTokens < 0 || quote.OutputTokens < 0 || price.MinInputTokens < 0 || quote.InputTokens < price.MinInputTokens || price.EffectiveAt < 0 || price.Currency != "USD" || (quote.QuantityCoverage != "partial" && quote.QuantityCoverage != "complete") {
		return errors.New("invalid accounting quote")
	}
	for _, identity := range []string{quote.TargetID, price.Provider, price.ExternalID, price.Tariff} {
		if identity == "" || identity != strings.TrimSpace(identity) {
			return errors.New("invalid accounting quote identity")
		}
	}
	if price.ExternalVersion != strings.TrimSpace(price.ExternalVersion) {
		return errors.New("invalid accounting quote version")
	}
	rates := []string{price.InputPerMillion, price.OutputPerMillion}
	for _, rate := range []*string{price.CacheReadPerMillion, price.CacheWritePerMillion, price.ReasoningPerMillion} {
		if rate != nil {
			rates = append(rates, *rate)
		}
	}
	for _, rate := range rates {
		if !priceDecimal.MatchString(rate) {
			return errors.New("invalid accounting quote rate")
		}
	}
	return nil
}

func cloneQuote(quote *Quote) *Quote {
	if quote == nil {
		return nil
	}
	copy := *quote
	clone := func(value *string) *string {
		if value == nil {
			return nil
		}
		text := *value
		return &text
	}
	copy.Price.CacheReadPerMillion = clone(quote.Price.CacheReadPerMillion)
	copy.Price.CacheWritePerMillion = clone(quote.Price.CacheWritePerMillion)
	copy.Price.ReasoningPerMillion = clone(quote.Price.ReasoningPerMillion)
	return &copy
}
