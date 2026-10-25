package config

import "testing"

func TestLedgerSettingsDefaultsAndValidation(t *testing.T) {
	for _, key := range []string{"SWOBU_LEDGER", "SWOBU_LEDGER_DETAIL_RETENTION_DAYS", "SWOBU_LEDGER_MAX_BYTES"} {
		t.Setenv(key, "")
	}
	settings, err := ResolveLedgerSettings()
	if err != nil || settings.Backend != "sqlite" || settings.DetailDays != 30 || settings.MaxBytes != 1073741824 {
		t.Fatalf("defaults=%#v, %v", settings, err)
	}
	for _, backend := range []string{"sqlite", "postgres", "off"} {
		t.Setenv("SWOBU_LEDGER", backend)
		if _, err := ResolveLedgerSettings(); err != nil {
			t.Fatalf("backend %q: %v", backend, err)
		}
	}
	t.Setenv("SWOBU_LEDGER", "postgres")
	t.Setenv("SWOBU_LEDGER_MAX_BYTES", "1024")
	if _, err := ResolveLedgerSettings(); err == nil {
		t.Fatal("PostgreSQL byte budget accepted")
	}
	t.Setenv("SWOBU_LEDGER_MAX_BYTES", "")
	t.Setenv("SWOBU_LEDGER", "sqlite")
	for _, value := range []string{"0", "-1", "not-a-number", "9223372036854775808"} {
		t.Setenv("SWOBU_LEDGER_DETAIL_RETENTION_DAYS", value)
		if _, err := ResolveLedgerSettings(); err == nil {
			t.Fatalf("invalid retention %q accepted", value)
		}
	}
	t.Setenv("SWOBU_LEDGER_DETAIL_RETENTION_DAYS", "30")
	t.Setenv("SWOBU_LEDGER", "unknown")
	if _, err := ResolveLedgerSettings(); err == nil {
		t.Fatal("unknown backend accepted")
	}
}
