package ledger

import "errors"

const (
	DefaultDetailRetentionDays       = 30
	DefaultSQLiteMaxBytes      int64 = 1_073_741_824
)

// RetentionConfig sets maximum desired horizons. SQLiteMaxBytes is a soft
// physical budget owned only by the SQLite adapter; nil means the backend has
// no byte-retention contract.
type RetentionConfig struct {
	DetailDays     int
	SQLiteMaxBytes *int64
}

func DefaultSQLiteRetention() RetentionConfig {
	maximum := DefaultSQLiteMaxBytes
	return RetentionConfig{DetailDays: DefaultDetailRetentionDays, SQLiteMaxBytes: &maximum}
}

func DefaultPostgresRetention() RetentionConfig {
	return RetentionConfig{DetailDays: DefaultDetailRetentionDays}
}

func (c RetentionConfig) Validate() error {
	if c.DetailDays <= 0 || c.SQLiteMaxBytes != nil && *c.SQLiteMaxBytes <= 0 {
		return errors.New("Ledger retention values must be positive")
	}
	return nil
}
