package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/wang4386/CDT-Monitor/internal/security"
)

const schema = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS accounts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    access_key_id TEXT NOT NULL DEFAULT '',
    access_key_secret TEXT NOT NULL DEFAULT '',
    region_id TEXT NOT NULL DEFAULT '',
    instance_id TEXT NOT NULL DEFAULT '',
	    public_ip TEXT NOT NULL DEFAULT '',
    public_ipv6 TEXT NOT NULL DEFAULT '',
	    ip_updated_at INTEGER NOT NULL DEFAULT 0,
    max_traffic REAL NOT NULL DEFAULT 0,
    schedule_enabled INTEGER NOT NULL DEFAULT 0,
    start_time TEXT NOT NULL DEFAULT '',
    stop_time TEXT NOT NULL DEFAULT '',
    traffic_used REAL NOT NULL DEFAULT 0,
    instance_status TEXT NOT NULL DEFAULT 'Unknown',
    updated_at INTEGER NOT NULL DEFAULT 0,
    last_keep_alive_at INTEGER NOT NULL DEFAULT 0,
    remark TEXT NOT NULL DEFAULT '',
    site_type TEXT NOT NULL DEFAULT 'china',
    deleted_at INTEGER NOT NULL DEFAULT 0,
	    daily_report_time TEXT NOT NULL DEFAULT '00:00',
	    start_cron TEXT NOT NULL DEFAULT '',
	    stop_cron TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    type TEXT NOT NULL,
    message TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_logs_type_created ON logs(type, created_at DESC);
CREATE TABLE IF NOT EXISTS login_attempts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ip TEXT NOT NULL,
    attempt_time INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_login_attempts_ip_time ON login_attempts(ip, attempt_time DESC);
CREATE TABLE IF NOT EXISTS traffic_hourly (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id INTEGER NOT NULL,
    traffic REAL NOT NULL,
    recorded_at INTEGER NOT NULL,
    UNIQUE(account_id, recorded_at)
);
CREATE TABLE IF NOT EXISTS traffic_daily (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id INTEGER NOT NULL,
    traffic REAL NOT NULL,
    recorded_at INTEGER NOT NULL,
    UNIQUE(account_id, recorded_at)
);
CREATE TABLE IF NOT EXISTS billing_cache (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    account_id INTEGER NOT NULL,
    cache_type TEXT NOT NULL,
    billing_cycle TEXT NOT NULL DEFAULT '',
    data TEXT NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(account_id, cache_type, billing_cycle)
);
CREATE TABLE IF NOT EXISTS sessions (
    token_hash TEXT PRIMARY KEY,
    ip TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);
CREATE TABLE IF NOT EXISTS api_keys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    scopes TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    last_used_at INTEGER,
    expires_at INTEGER,
    revoked_at INTEGER
);
CREATE TABLE IF NOT EXISTS passkeys (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    credential_id BLOB NOT NULL UNIQUE,
    credential_json TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    last_used_at INTEGER
);
CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    account_id INTEGER NOT NULL DEFAULT 0,
    payload TEXT NOT NULL DEFAULT '{}',
    unique_key TEXT UNIQUE,
    status TEXT NOT NULL DEFAULT 'queued',
    result TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    available_at INTEGER NOT NULL,
    locked_at INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_jobs_claim ON jobs(status, available_at, created_at);
CREATE TABLE IF NOT EXISTS action_events (
    event_key TEXT PRIMARY KEY,
    account_id INTEGER NOT NULL,
    type TEXT NOT NULL,
    status TEXT NOT NULL,
    detail TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS scheduler_leases (
    name TEXT PRIMARY KEY,
    owner TEXT NOT NULL,
    expires_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS notification_outbox (
    id TEXT PRIMARY KEY,
    event_id TEXT NOT NULL,
    channel TEXT NOT NULL,
    payload TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 5,
    available_at INTEGER NOT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(event_id, channel)
);
CREATE INDEX IF NOT EXISTS idx_outbox_claim ON notification_outbox(status, available_at, created_at);
CREATE TABLE IF NOT EXISTS daily_traffic_snapshots (
    account_id INTEGER NOT NULL,
    date_str TEXT NOT NULL,
    start_traffic REAL NOT NULL DEFAULT -1,
    stop_traffic REAL NOT NULL DEFAULT -1,
    start_time TEXT NOT NULL DEFAULT '',
    stop_time TEXT NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(account_id, date_str)
);
`

func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("apply base schema: %w", err)
	}
	for _, col := range []struct{ name, definition string }{
		{"remark", "TEXT NOT NULL DEFAULT ''"},
		{"site_type", "TEXT NOT NULL DEFAULT 'china'"},
		{"deleted_at", "INTEGER NOT NULL DEFAULT 0"},
		{"public_ip", "TEXT NOT NULL DEFAULT ''"},
		{"public_ipv6", "TEXT NOT NULL DEFAULT ''"},
		{"ip_updated_at", "INTEGER NOT NULL DEFAULT 0"},
		{"keep_alive", "INTEGER"},
		{"shutdown_mode", "TEXT NOT NULL DEFAULT ''"},
		{"daily_report", "INTEGER"},
		{"daily_report_time", "TEXT NOT NULL DEFAULT '00:00'"},
		{"start_cron", "TEXT NOT NULL DEFAULT ''"},
		{"stop_cron", "TEXT NOT NULL DEFAULT ''"},
		{"schedule_mode", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := s.ensureColumn(ctx, "accounts", col.name, col.definition); err != nil {
			return err
		}
	}
	if err := s.migrateLegacyStats(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(1, unixepoch())`)
	return err
}

func (s *Store) migrateLegacyStats(ctx context.Context) error {
	for _, table := range []string{"traffic_hourly", "traffic_daily"} {
		columns, err := s.tableColumns(ctx, table)
		if err != nil {
			return err
		}
		if columns["account_id"] || !columns["access_key_id"] {
			continue
		}
		legacyTable := table + "_legacy_v1"
		if err = s.WithTx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `ALTER TABLE `+table+` RENAME TO `+legacyTable); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `CREATE TABLE `+table+` (id INTEGER PRIMARY KEY AUTOINCREMENT, account_id INTEGER NOT NULL, traffic REAL NOT NULL, recorded_at INTEGER NOT NULL, UNIQUE(account_id, recorded_at))`); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO `+table+`(account_id,traffic,recorded_at) SELECT a.id,t.traffic,t.recorded_at FROM `+legacyTable+` t JOIN accounts a ON a.access_key_id=t.access_key_id WHERE a.deleted_at=0`); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `DROP TABLE `+legacyTable); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_`+table+`_unique ON `+table+`(account_id,recorded_at)`)
			return err
		}); err != nil {
			return fmt.Errorf("migrate legacy %s: %w", table, err)
		}
	}
	return nil
}

func (s *Store) tableColumns(ctx context.Context, table string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := make(map[string]bool)
	for rows.Next() {
		var cid, notNull, pk int
		var name, dataType string
		var defaultValue sql.NullString
		if err = rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return nil, err
		}
		columns[name] = true
	}
	return columns, rows.Err()
}

func (s *Store) ensureColumn(ctx context.Context, table, column, definition string) error {
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultValue sql.NullString
		if err = rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	_, err = s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}

func (s *Store) migratePlaintextSecrets(ctx context.Context) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		for _, key := range []string{"notify_password", "notify_tg_token", "notify_tg_proxy_pass", "notify_wh_headers"} {
			var value string
			err := tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&value)
			if err == sql.ErrNoRows || value == "" || security.IsEncrypted(value) {
				continue
			}
			if err != nil {
				return err
			}
			encrypted, err := s.Encrypt(value)
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE settings SET value=? WHERE key=?`, encrypted, key); err != nil {
				return err
			}
		}
		rows, err := tx.QueryContext(ctx, `SELECT id, access_key_secret FROM accounts WHERE access_key_secret != ''`)
		if err != nil {
			return err
		}
		type item struct {
			id     int64
			secret string
		}
		var items []item
		for rows.Next() {
			var it item
			if err = rows.Scan(&it.id, &it.secret); err != nil {
				rows.Close()
				return err
			}
			items = append(items, it)
		}
		rows.Close()
		for _, it := range items {
			if security.IsEncrypted(it.secret) {
				continue
			}
			encrypted, err := s.Encrypt(it.secret)
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE accounts SET access_key_secret=? WHERE id=?`, encrypted, it.id); err != nil {
				return err
			}
		}
		return nil
	})
}
