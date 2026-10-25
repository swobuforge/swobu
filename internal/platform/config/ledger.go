package config

import (
	"errors"
	"strconv"
)

type LedgerSettings struct {
	Backend    string
	DetailDays int
	MaxBytes   int64
}

func ResolveLedgerSettings() (LedgerSettings, error) {
	settings := LedgerSettings{Backend: ReadEnvTrim("SWOBU_LEDGER"), DetailDays: 30, MaxBytes: 1073741824}
	if settings.Backend == "" {
		settings.Backend = "sqlite"
	}
	if settings.Backend != "sqlite" && settings.Backend != "postgres" && settings.Backend != "off" {
		return LedgerSettings{}, errors.New("SWOBU_LEDGER must be sqlite, postgres or off")
	}
	if value := ReadEnvTrim("SWOBU_LEDGER_DETAIL_RETENTION_DAYS"); value != "" {
		days, err := strconv.Atoi(value)
		if err != nil || days <= 0 {
			return LedgerSettings{}, errors.New("Ledger detail retention must be a positive integer")
		}
		settings.DetailDays = days
	}
	if value := ReadEnvTrim("SWOBU_LEDGER_MAX_BYTES"); value != "" {
		if settings.Backend == "postgres" {
			return LedgerSettings{}, errors.New("PostgreSQL Ledger does not support a byte budget")
		}
		maximum, err := strconv.ParseInt(value, 10, 64)
		if err != nil || maximum <= 0 {
			return LedgerSettings{}, errors.New("Ledger byte budget must be a positive integer")
		}
		settings.MaxBytes = maximum
	}
	return settings, nil
}
