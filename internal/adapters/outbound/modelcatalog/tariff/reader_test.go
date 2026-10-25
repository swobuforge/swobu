package tariff

import (
	"database/sql"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/accounting"
	modernsqlite "modernc.org/sqlite"
)

func tariffFixture(t *testing.T) (*Reader, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "models.sqlite")
	connector, err := modernsqlite.NewConnector(path)
	if err != nil {
		t.Fatal(err)
	}
	database := sql.OpenDB(connector)
	t.Cleanup(func() { _ = database.Close() })
	_, err = database.Exec(`
CREATE TABLE provider_models (id INTEGER PRIMARY KEY, provider_id TEXT, external_id TEXT, external_version TEXT);
CREATE TABLE tariffs (id INTEGER PRIMARY KEY, provider_model_id INTEGER, name TEXT);
CREATE TABLE tariff_prices (tariff_id INTEGER, effective_at INTEGER, min_input_tokens INTEGER, currency TEXT,
 input_per_million TEXT, output_per_million TEXT, cache_read_per_million TEXT,
 cache_write_per_million TEXT, reasoning_per_million TEXT);
INSERT INTO provider_models VALUES (1,'seller','selector','v1'), (2,'other','selector','v1');
INSERT INTO tariffs VALUES (1,1,'regional'),(2,1,'standard'),(3,2,'regional');
INSERT INTO tariff_prices VALUES
 (1,100,0,'USD','1','2',NULL,NULL,NULL),
 (1,100,100,'USD','3','4','0',NULL,NULL),
 (1,200,0,'USD','5','6',NULL,NULL,NULL),
 (1,200,1000,'USD','7','8',NULL,NULL,NULL),
 (2,100,0,'USD','99','99',NULL,NULL,NULL),
 (3,100,0,'USD','88','88',NULL,NULL,NULL);`)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	return reader, database
}

func TestExactTariffSelectionPinsVersionBeforeWholeRequestBand(t *testing.T) {
	reader, _ := tariffFixture(t)
	for _, example := range []struct {
		at, tokens, effective, band int64
		rate                        string
	}{{100, 99, 100, 0, "1"}, {199, 100, 100, 100, "3"}, {200, 100, 200, 0, "5"}, {201, 1000, 200, 1000, "7"}} {
		version, err := reader.Select(t.Context(), Selection{Provider: "seller", ExternalID: "selector", ExternalVersion: "v1", Tariff: "regional", At: time.Unix(example.at, 0), InputTokens: example.tokens})
		if err != nil || version.EffectiveAt != example.effective || version.MinInputTokens != example.band || version.InputPerMillion != example.rate || version.Tariff != "regional" || version.Provider != "seller" || version.ExternalVersion != "v1" {
			t.Fatalf("exact version/band lost: %#v err=%v", version, err)
		}
		if example.band == 100 && (version.CacheReadPerMillion == nil || *version.CacheReadPerMillion != "0" || version.CacheWritePerMillion != nil) {
			t.Fatal("explicit zero and unknown optional rates merged")
		}
	}
}

func TestExactTariffMissingIdentityNeverFallsBack(t *testing.T) {
	reader, _ := tariffFixture(t)
	base := Selection{Provider: "seller", ExternalID: "selector", ExternalVersion: "v1", Tariff: "regional", At: time.Unix(150, 0), InputTokens: 1}
	for _, field := range []string{"provider", "selector", "version", "tariff", "before-history", "whitespace", "quantity"} {
		selection := base
		switch field {
		case "provider":
			selection.Provider = "unknown"
		case "selector":
			selection.ExternalID = "Selector"
		case "version":
			selection.ExternalVersion = ""
		case "tariff":
			selection.Tariff = ""
		case "before-history":
			selection.At = time.Unix(99, 0)
		case "whitespace":
			selection.Provider = "seller "
		case "quantity":
			selection.InputTokens = -1
		}
		if _, err := reader.Select(t.Context(), selection); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("%s selected substitute: %v", field, err)
		}
	}
}

func TestInvalidCurrentTariffCannotResurrectHistoricalEconomics(t *testing.T) {
	for _, mutation := range []string{
		"DELETE FROM tariff_prices WHERE tariff_id=1 AND effective_at=200 AND min_input_tokens=0",
		"UPDATE tariff_prices SET input_per_million='NaN' WHERE tariff_id=1 AND effective_at=200 AND min_input_tokens=1000",
		"INSERT INTO tariff_prices SELECT * FROM tariff_prices WHERE tariff_id=1 AND effective_at=200 AND min_input_tokens=0",
		"UPDATE tariff_prices SET currency='EUR' WHERE tariff_id=1 AND effective_at=200 AND min_input_tokens=1000",
	} {
		t.Run(mutation, func(t *testing.T) {
			reader, database := tariffFixture(t)
			if _, err := database.Exec(mutation); err != nil {
				t.Fatal(err)
			}
			if _, err := reader.Select(t.Context(), Selection{Provider: "seller", ExternalID: "selector", ExternalVersion: "v1", Tariff: "regional", At: time.Unix(250, 0), InputTokens: 1}); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("invalid current version supplied economics: %v", err)
			}
		})
	}
}

func TestOrdinaryUSDCostIsExactAndRoundsOnceConservatively(t *testing.T) {
	for _, example := range []struct {
		input, output                   string
		inputTokens, outputTokens, want int64
	}{
		{"0", "0", 1, 1, 0},
		{"0.000000000000000001", "0", 1, 0, 1},
		{"1", "1", 1, 1, 1},
		{"1.000000000000000001", "0", 10000, 0, 2},
		{"9007199254740993", "0", 10000, 0, 9007199254740993},
		{"3", "4", 1000000, 1000000, 700},
	} {
		version := Version{Currency: "USD", InputPerMillion: example.input, OutputPerMillion: example.output}
		got, err := version.OrdinaryUSDCents(example.inputTokens, example.outputTokens)
		if err != nil || got != example.want {
			t.Fatalf("exact cents=%d want=%d err=%v", got, example.want, err)
		}
	}
	for _, version := range []Version{
		{Currency: "EUR", InputPerMillion: "1", OutputPerMillion: "1"},
		{Currency: "USD", InputPerMillion: "1e2", OutputPerMillion: "1"},
		{Currency: "USD", InputPerMillion: "-1", OutputPerMillion: "1"},
		{Currency: "USD", InputPerMillion: "9223372036854775808", OutputPerMillion: "0"},
	} {
		if _, err := version.OrdinaryUSDCents(10000, 0); !errors.Is(err, ErrUnavailable) {
			t.Fatal("invalid currency/rate/overflow treated as known cost")
		}
	}
	version := Version{Currency: "USD", InputPerMillion: "1", OutputPerMillion: "1"}
	if _, err := version.OrdinaryUSDCents(-1, math.MaxInt64); !errors.Is(err, ErrUnavailable) {
		t.Fatal("negative quantity priced")
	}
}

func TestTariffReaderCannotCreateOrWriteCatalogue(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.sqlite")
	if reader, err := Open(t.Context(), missing); err == nil {
		_ = reader.Close()
		t.Fatal("missing catalogue created")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("reader created missing catalogue")
	}
	reader, _ := tariffFixture(t)
	if _, err := reader.database.Exec("DELETE FROM tariff_prices"); err == nil {
		t.Fatal("reader can mutate accepted catalogue")
	}
}

func TestAcceptedCatalogueEveryHistoricalTariffBandIsReadable(t *testing.T) {
	path := os.Getenv("SWOBU_TEST_MODELS_DATABASE")
	if path == "" {
		t.Skip("set SWOBU_TEST_MODELS_DATABASE to qualify the accepted Models catalogue")
	}
	reader, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	rows, err := reader.database.QueryContext(t.Context(), `SELECT offer.provider_id, offer.external_id, offer.external_version, tariff.name,
price.effective_at, price.min_input_tokens, price.currency, price.input_per_million, price.output_per_million
FROM provider_models offer JOIN tariffs tariff ON tariff.provider_model_id=offer.id
JOIN tariff_prices price ON price.tariff_id=tariff.id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var expected Version
		if err := rows.Scan(&expected.Provider, &expected.ExternalID, &expected.ExternalVersion, &expected.Tariff, &expected.EffectiveAt, &expected.MinInputTokens, &expected.Currency, &expected.InputPerMillion, &expected.OutputPerMillion); err != nil {
			t.Fatal(err)
		}
		actual, err := reader.Select(t.Context(), Selection{Provider: expected.Provider, ExternalID: expected.ExternalID, ExternalVersion: expected.ExternalVersion, Tariff: expected.Tariff, At: time.Unix(expected.EffectiveAt, 0), InputTokens: expected.MinInputTokens})
		if err != nil || actual.EffectiveAt != expected.EffectiveAt || actual.MinInputTokens != expected.MinInputTokens || actual.Currency != expected.Currency || actual.InputPerMillion != expected.InputPerMillion || actual.OutputPerMillion != expected.OutputPerMillion {
			t.Fatalf("accepted band unreadable: %#v actual=%#v err=%v", expected, actual, err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("accepted catalogue has no tariff bands")
	}
	t.Logf("qualified %d accepted historical tariff bands", count)
}

func TestSelectedTariffQuotePersistsBeforeEffectAndSurvivesPriceChange(t *testing.T) {
	reader, database := tariffFixture(t)
	version, err := reader.Select(t.Context(), Selection{Provider: "seller", ExternalID: "selector", ExternalVersion: "v1", Tariff: "regional", At: time.Unix(150, 0), InputTokens: 10000})
	if err != nil {
		t.Fatal(err)
	}
	quote, err := version.Quote("target", 1, 10000, 10000, "partial")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "accounting.json")
	store, err := accounting.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	reservation, err := store.ReserveQuote("coding", "attempt", quote)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := accounting.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	stored := reopened.Reservations("coding")
	if len(stored) != 1 || stored[0].State != "reserved" || !reflect.DeepEqual(stored[0].Quote, &quote) || stored[0].Coverage != accounting.CoverageKnown || stored[0].Quote.QuantityCoverage != "partial" {
		t.Fatal("admitted price/quantity provenance was not durable before effect")
	}
	if _, err := database.Exec("UPDATE tariff_prices SET input_per_million='999' WHERE tariff_id=1"); err != nil {
		t.Fatal(err)
	}
	if err := reopened.SettleReservation("coding", reservation.WindowKey, "attempt", 100, true); err != nil {
		t.Fatal(err)
	}
	after, err := accounting.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	stored = after.Reservations("coding")
	if len(stored) != 1 || stored[0].AmountCents != 100 || stored[0].State != "uncertain" || !reflect.DeepEqual(stored[0].Quote, &quote) {
		t.Fatal("source price change repriced historical effect")
	}
}
