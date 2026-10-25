package tariff

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/accounting"
	modernsqlite "modernc.org/sqlite"
)

var ErrUnavailable = errors.New("exact tariff version is unavailable")

var decimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]+)?$`)
var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

// Selection names provider-local economics independently of canonical Model
// joins and comparison roles. InputTokens selects the whole-request band.
type Selection struct {
	Provider        string
	ExternalID      string
	ExternalVersion string
	Tariff          string
	At              time.Time
	InputTokens     int64
}

// Version retains the selected economic identity and exact native-currency
// vector. Nil optional rates mean unknown, not inherited or zero.
type Version struct {
	Provider             string
	ExternalID           string
	ExternalVersion      string
	Tariff               string
	EffectiveAt          int64
	MinInputTokens       int64
	Currency             string
	InputPerMillion      string
	OutputPerMillion     string
	CacheReadPerMillion  *string
	CacheWritePerMillion *string
	ReasoningPerMillion  *string
}

type Reader struct{ database *sql.DB }

func Open(ctx context.Context, path string) (*Reader, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("tariff database path must be absolute")
	}
	uriPath := filepath.ToSlash(path)
	// Drive-qualified Windows paths must remain URI paths, not authorities.
	if filepath.VolumeName(path) != "" && !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	location := &url.URL{Scheme: "file", Path: uriPath, RawQuery: "mode=ro"}
	connector, err := modernsqlite.NewConnector(location.String())
	if err != nil {
		return nil, err
	}
	database := sql.OpenDB(connector)
	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		return nil, err
	}
	return &Reader{database: database}, nil
}

func (reader *Reader) Close() error { return reader.database.Close() }

func (reader *Reader) Select(ctx context.Context, selection Selection) (Version, error) {
	if selection.Provider == "" || selection.ExternalID == "" || selection.Tariff == "" || selection.At.IsZero() || selection.At.Unix() < 0 || selection.InputTokens < 0 {
		return Version{}, ErrUnavailable
	}
	for _, identity := range []string{selection.Provider, selection.ExternalID, selection.ExternalVersion, selection.Tariff} {
		if identity != strings.TrimSpace(identity) {
			return Version{}, ErrUnavailable
		}
	}
	rows, err := reader.database.QueryContext(ctx, `
SELECT price.effective_at, price.min_input_tokens, price.currency,
       price.input_per_million, price.output_per_million,
       price.cache_read_per_million, price.cache_write_per_million,
       price.reasoning_per_million
FROM provider_models offer
JOIN tariffs tariff ON tariff.provider_model_id = offer.id
JOIN tariff_prices price ON price.tariff_id = tariff.id
WHERE offer.provider_id = ? AND offer.external_id = ?
  AND offer.external_version = ? AND tariff.name = ?
  AND price.effective_at = (
    SELECT MAX(history.effective_at) FROM tariff_prices history
    WHERE history.tariff_id = tariff.id AND history.effective_at <= ?)
ORDER BY price.min_input_tokens`, selection.Provider, selection.ExternalID, selection.ExternalVersion, selection.Tariff, selection.At.Unix())
	if err != nil {
		return Version{}, fmt.Errorf("read tariff: %w", err)
	}
	defer rows.Close()
	var selected Version
	previous := int64(-1)
	currency := ""
	found := false
	for rows.Next() {
		version := Version{Provider: selection.Provider, ExternalID: selection.ExternalID, ExternalVersion: selection.ExternalVersion, Tariff: selection.Tariff}
		if err := rows.Scan(&version.EffectiveAt, &version.MinInputTokens, &version.Currency, &version.InputPerMillion, &version.OutputPerMillion, &version.CacheReadPerMillion, &version.CacheWritePerMillion, &version.ReasoningPerMillion); err != nil {
			return Version{}, err
		}
		if (previous == -1 && version.MinInputTokens != 0) || version.MinInputTokens <= previous || version.validate() != nil {
			return Version{}, ErrUnavailable
		}
		previous = version.MinInputTokens
		if currency != "" && currency != version.Currency {
			return Version{}, ErrUnavailable
		}
		currency = version.Currency
		if version.MinInputTokens <= selection.InputTokens {
			selected, found = version, true
		}
	}
	if err := rows.Err(); err != nil {
		return Version{}, err
	}
	if !found {
		return Version{}, ErrUnavailable
	}
	return selected, nil
}

func (version Version) validate() error {
	if version.EffectiveAt < 0 || version.MinInputTokens < 0 || !currencyPattern.MatchString(version.Currency) {
		return ErrUnavailable
	}
	rates := []string{version.InputPerMillion, version.OutputPerMillion}
	for _, optional := range []*string{version.CacheReadPerMillion, version.CacheWritePerMillion, version.ReasoningPerMillion} {
		if optional != nil {
			rates = append(rates, *optional)
		}
	}
	for _, rate := range rates {
		if !decimalPattern.MatchString(rate) {
			return ErrUnavailable
		}
	}
	return nil
}

// OrdinaryUSDCents prices uncached input and ordinary output only. Quantities
// must exclude separately billable cache/reasoning items; callers own that
// semantic proof. It rounds the combined exact cost upward once to whole cents.
func (version Version) OrdinaryUSDCents(inputTokens, outputTokens int64) (int64, error) {
	if version.validate() != nil || version.Currency != "USD" || inputTokens < version.MinInputTokens || inputTokens < 0 || outputTokens < 0 {
		return 0, ErrUnavailable
	}
	inputRate, _ := new(big.Rat).SetString(version.InputPerMillion)
	outputRate, _ := new(big.Rat).SetString(version.OutputPerMillion)
	inputCost := new(big.Rat).Mul(inputRate, new(big.Rat).SetInt64(inputTokens))
	outputCost := new(big.Rat).Mul(outputRate, new(big.Rat).SetInt64(outputTokens))
	cents := new(big.Rat).Quo(new(big.Rat).Add(inputCost, outputCost), big.NewRat(10000, 1))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(cents.Num(), cents.Denom(), remainder)
	if remainder.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return 0, ErrUnavailable
	}
	return quotient.Int64(), nil
}

// Quote carries the selected version into synchronous reservation storage.
// Coverage describes quantity forecasting, independently of known unit rates.
func (version Version) Quote(targetID string, targetVersion uint64, inputTokens, outputTokens int64, quantityCoverage string) (accounting.Quote, error) {
	amount, err := version.OrdinaryUSDCents(inputTokens, outputTokens)
	if err != nil {
		return accounting.Quote{}, err
	}
	quote := accounting.Quote{
		Price: accounting.PriceSnapshot{
			Provider: version.Provider, ExternalID: version.ExternalID, ExternalVersion: version.ExternalVersion,
			Tariff: version.Tariff, EffectiveAt: version.EffectiveAt, MinInputTokens: version.MinInputTokens,
			Currency: version.Currency, InputPerMillion: version.InputPerMillion, OutputPerMillion: version.OutputPerMillion,
			CacheReadPerMillion: version.CacheReadPerMillion, CacheWritePerMillion: version.CacheWritePerMillion, ReasoningPerMillion: version.ReasoningPerMillion,
		},
		TargetID: targetID, TargetVersion: targetVersion, AmountCents: amount,
		InputTokens: inputTokens, OutputTokens: outputTokens, QuantityCoverage: quantityCoverage,
	}
	if err := quote.Validate(); err != nil {
		return accounting.Quote{}, err
	}
	return quote, nil
}
