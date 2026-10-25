package bootstrap

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	ledgerpostgres "github.com/swobuforge/swobu/internal/adapters/outbound/ledger/postgres"
	ledgersqlite "github.com/swobuforge/swobu/internal/adapters/outbound/ledger/sqlite"
	"github.com/swobuforge/swobu/internal/ledger"
	"github.com/swobuforge/swobu/internal/platform/config"
)

func (daemon *Daemon) startLedger(ctx context.Context) error {
	settings, err := config.ResolveLedgerSettings()
	if err != nil {
		return err
	}
	if settings.Backend == "off" {
		return nil
	}
	var store ledger.Store
	retention := ledger.RetentionConfig{DetailDays: settings.DetailDays}
	if settings.Backend == "sqlite" {
		retention.SQLiteMaxBytes = &settings.MaxBytes
		name := "ledger.sqlite3"
		if !daemon.configStore.ReadOnly() {
			// Managed Nodes retain the established file name beside their YAML.
			stem := strings.TrimSuffix(filepath.Base(daemon.configPath), filepath.Ext(daemon.configPath))
			name = stem + ".ledger.sqlite3"
		}
		// Externally managed config may live on an immutable mount. All
		// generated runtime evidence belongs to the canonical Node runtime.
		path, pathErr := filepath.Abs(filepath.Join(daemon.runtimeDir, name))
		if pathErr != nil {
			return pathErr
		}
		store, err = ledgersqlite.Open(ctx, ledgersqlite.Config{Path: path, Retention: retention, CreatedByVersion: "daemon"})
	} else {
		store, err = ledgerpostgres.Open(ctx, ledgerpostgres.Config{DSN: config.ReadEnvTrim("SWOBU_LEDGER_POSTGRES_DSN"), Retention: retention, CreatedByVersion: "daemon"})
	}
	if err != nil {
		daemon.ledgerOpenFailed = true
		code := ledger.ErrorOpenFailed
		var openErr *ledgersqlite.OpenError
		if errors.As(err, &openErr) {
			code = openErr.Code
		}
		daemon.logger.Warn("Ledger unavailable", "component", "ledger", "code", code)
		return nil
	}
	writer, err := ledger.NewWriter(store, ledger.WriterConfig{})
	if err != nil {
		return errors.Join(err, store.Close())
	}
	daemon.ledgerStore, daemon.ledgerWriter = store, writer
	return nil
}
