package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"itagent/internal/server/model"
)

const schema = `
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS devices (
    device_id     TEXT PRIMARY KEY,
    device_token  TEXT NOT NULL,
    hostname      TEXT NOT NULL DEFAULT '',
    os            TEXT NOT NULL DEFAULT '',
    agent_version TEXT NOT NULL DEFAULT '',
    last_seen_at  DATETIME NOT NULL,
    registered_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS reports (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id   TEXT NOT NULL REFERENCES devices(device_id),
    report_type TEXT NOT NULL,
    payload     BLOB NOT NULL,
    reported_at DATETIME NOT NULL,
    received_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_reports_device ON reports(device_id, id DESC);

CREATE TABLE IF NOT EXISTS snapshots (
    device_id  TEXT PRIMARY KEY REFERENCES devices(device_id),
    payload    BLOB NOT NULL,
    updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS change_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id  TEXT NOT NULL REFERENCES devices(device_id),
    kind       TEXT NOT NULL,
    severity   TEXT NOT NULL,
    message    TEXT NOT NULL,
    detail     BLOB,
    acked      INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_change_events_open ON change_events(acked, id DESC);

CREATE TABLE IF NOT EXISTS protection_modules (
    module_key    TEXT PRIMARY KEY,
    enabled       INTEGER NOT NULL DEFAULT 0,
    password_hash TEXT NOT NULL DEFAULT '',
    updated_at    DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS uninstall_codes (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    code       TEXT NOT NULL UNIQUE,
    device_id  TEXT NOT NULL,
    expires_at DATETIME NOT NULL,
    used_at    DATETIME,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    deleted_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_uninstall_codes_device ON uninstall_codes(device_id, code);

CREATE TABLE IF NOT EXISTS asset_dispatches (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    company_id         INTEGER NOT NULL,
    asset_id           INTEGER NOT NULL,
    borrower_name      TEXT NOT NULL DEFAULT '',
    destination        TEXT NOT NULL DEFAULT '',
    dispatched_at      DATETIME NOT NULL,
    expected_return_at DATETIME NOT NULL,
    returned_at        DATETIME,
    isolation_offline  INTEGER NOT NULL DEFAULT 0,
    expect_wipe        INTEGER NOT NULL DEFAULT 0,
    status             INTEGER NOT NULL DEFAULT 10,
    remark             TEXT NOT NULL DEFAULT '',
    created_at         DATETIME NOT NULL,
    updated_at         DATETIME NOT NULL,
    deleted_at         DATETIME
);

CREATE INDEX IF NOT EXISTS idx_asset_dispatches_company_status ON asset_dispatches(company_id, status);
CREATE INDEX IF NOT EXISTS idx_asset_dispatches_asset_status ON asset_dispatches(asset_id, status);
CREATE INDEX IF NOT EXISTS idx_asset_dispatches_return_by ON asset_dispatches(expected_return_at);

CREATE TABLE IF NOT EXISTS webhook_alert_config (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    enabled          INTEGER NOT NULL DEFAULT 0,
    webhook_url      TEXT NOT NULL DEFAULT '',
    secret           TEXT NOT NULL DEFAULT '',
    cooldown_minutes INTEGER NOT NULL DEFAULT 60,
    updated_at       DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS agent_settings (
    id                    INTEGER PRIMARY KEY,
    heartbeat_interval_sec INTEGER NOT NULL,
    full_interval_sec      INTEGER NOT NULL,
    offline_threshold_sec  INTEGER NOT NULL,
    updated_at            DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS webhook_alert_states (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    company_id INTEGER NOT NULL,
    asset_id   INTEGER NOT NULL,
    alert_type TEXT NOT NULL,
    sent_at    DATETIME NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    deleted_at DATETIME
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_webhook_alert_state ON webhook_alert_states(company_id, asset_id, alert_type);

CREATE TABLE IF NOT EXISTS stocktakes (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    company_id      INTEGER NOT NULL,
    name            TEXT NOT NULL,
    remark          TEXT NOT NULL DEFAULT '',
    status          INTEGER NOT NULL DEFAULT 10,
    started_at      DATETIME,
    finished_at     DATETIME,
    created_by      INTEGER,
    scan_token_hash TEXT NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL,
    deleted_at      DATETIME
);

CREATE INDEX IF NOT EXISTS idx_stocktakes_company_status ON stocktakes(company_id, status);
CREATE INDEX IF NOT EXISTS idx_stocktakes_token ON stocktakes(scan_token_hash);

CREATE TABLE IF NOT EXISTS stocktake_items (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    company_id        INTEGER NOT NULL,
    stocktake_id      INTEGER NOT NULL,
    asset_id          INTEGER NOT NULL,
    asset_tag         TEXT NOT NULL,
    expected_location TEXT NOT NULL DEFAULT '',
    expected_status   INTEGER NOT NULL DEFAULT 10,
    actual_location   TEXT NOT NULL DEFAULT '',
    result            INTEGER NOT NULL DEFAULT 10,
    scanned_by        TEXT NOT NULL DEFAULT '',
    scanned_at        DATETIME,
    remark            TEXT NOT NULL DEFAULT '',
    created_at        DATETIME NOT NULL,
    updated_at        DATETIME NOT NULL,
    deleted_at        DATETIME
);

CREATE INDEX IF NOT EXISTS idx_stocktake_items_task_result ON stocktake_items(stocktake_id, result);
CREATE UNIQUE INDEX IF NOT EXISTS uk_stocktake_items_task_asset ON stocktake_items(stocktake_id, asset_id);
CREATE INDEX IF NOT EXISTS idx_stocktake_items_company ON stocktake_items(company_id);

CREATE TABLE IF NOT EXISTS asset_requests (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    company_id         INTEGER NOT NULL,
    asset_id           INTEGER NOT NULL,
    applicant_id       INTEGER NOT NULL,
    applicant_name     TEXT NOT NULL DEFAULT '',
    is_long_term       INTEGER NOT NULL DEFAULT 0,
    expected_return_at DATETIME,
    reason             TEXT NOT NULL DEFAULT '',
    status             INTEGER NOT NULL DEFAULT 10,
    approved_by        INTEGER,
    approved_at        DATETIME,
    decision_remark    TEXT NOT NULL DEFAULT '',
    created_at         DATETIME NOT NULL,
    updated_at         DATETIME NOT NULL,
    deleted_at         DATETIME
);

CREATE INDEX IF NOT EXISTS idx_asset_requests_company_status ON asset_requests(company_id, status);
CREATE INDEX IF NOT EXISTS idx_asset_requests_asset_status ON asset_requests(asset_id, status);
CREATE INDEX IF NOT EXISTS idx_asset_requests_applicant ON asset_requests(applicant_id);

CREATE TABLE IF NOT EXISTS depreciations (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    company_id  INTEGER NOT NULL,
    name        TEXT NOT NULL,
    months      INTEGER NOT NULL,
    floor_type  TEXT NOT NULL DEFAULT 'percent',
    floor_val   REAL NOT NULL DEFAULT 0,
    stages      TEXT NOT NULL DEFAULT '',
    enabled     INTEGER NOT NULL DEFAULT 1,
    remark      TEXT NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL,
    deleted_at  DATETIME
);

CREATE INDEX IF NOT EXISTS idx_depreciations_company ON depreciations(company_id);
CREATE INDEX IF NOT EXISTS idx_depreciations_enabled ON depreciations(enabled);

CREATE TABLE IF NOT EXISTS manufacturers (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    company_id INTEGER NOT NULL,
    name       TEXT NOT NULL,
    remark     TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    deleted_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_manufacturers_company ON manufacturers(company_id);

CREATE TABLE IF NOT EXISTS suppliers (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    company_id   INTEGER NOT NULL,
    name         TEXT NOT NULL,
    contact_name TEXT NOT NULL DEFAULT '',
    phone        TEXT NOT NULL DEFAULT '',
    remark       TEXT NOT NULL DEFAULT '',
    created_at   DATETIME NOT NULL,
    updated_at   DATETIME NOT NULL,
    deleted_at   DATETIME
);

CREATE INDEX IF NOT EXISTS idx_suppliers_company ON suppliers(company_id);

CREATE TABLE IF NOT EXISTS locations (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    company_id INTEGER NOT NULL,
    name       TEXT NOT NULL,
    parent_id  INTEGER,
    remark     TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    deleted_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_locations_company ON locations(company_id);
CREATE INDEX IF NOT EXISTS idx_locations_parent ON locations(parent_id);

CREATE TABLE IF NOT EXISTS asset_models (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    company_id      INTEGER NOT NULL,
    name            TEXT NOT NULL,
    category_id     INTEGER NOT NULL DEFAULT 0,
    manufacturer_id INTEGER,
    depreciation_id INTEGER,
    eol_months      INTEGER NOT NULL DEFAULT 0,
    remark          TEXT NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL,
    deleted_at      DATETIME
);

CREATE INDEX IF NOT EXISTS idx_asset_models_company ON asset_models(company_id);
CREATE INDEX IF NOT EXISTS idx_asset_models_category ON asset_models(category_id);
CREATE INDEX IF NOT EXISTS idx_asset_models_manufacturer ON asset_models(manufacturer_id);
CREATE INDEX IF NOT EXISTS idx_asset_models_depreciation ON asset_models(depreciation_id);

CREATE TABLE IF NOT EXISTS operation_logs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    company_id  INTEGER NOT NULL DEFAULT 0,
    user_id     INTEGER NOT NULL DEFAULT 0,
    username    TEXT NOT NULL DEFAULT '',
    role        TEXT NOT NULL DEFAULT '',
    action      TEXT NOT NULL,
    resource    TEXT NOT NULL,
    resource_id TEXT NOT NULL DEFAULT '',
    path        TEXT NOT NULL DEFAULT '',
    detail      TEXT NOT NULL DEFAULT '',
    ip          TEXT NOT NULL DEFAULT '',
    user_agent  TEXT NOT NULL DEFAULT '',
    status      INTEGER NOT NULL DEFAULT 0,
    created_at  DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_operation_logs_company ON operation_logs(company_id);
CREATE INDEX IF NOT EXISTS idx_operation_logs_user ON operation_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_operation_logs_action ON operation_logs(action);
CREATE INDEX IF NOT EXISTS idx_operation_logs_resource ON operation_logs(resource);
CREATE INDEX IF NOT EXISTS idx_operation_logs_resource_id ON operation_logs(resource_id);
CREATE INDEX IF NOT EXISTS idx_operation_logs_created_at ON operation_logs(created_at);

CREATE TABLE IF NOT EXISTS notifications (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    company_id  INTEGER NOT NULL,
    user_id     INTEGER NOT NULL,
    type        TEXT NOT NULL,
    title       TEXT NOT NULL,
    content     TEXT NOT NULL DEFAULT '',
    resource    TEXT NOT NULL DEFAULT '',
    resource_id TEXT NOT NULL DEFAULT '',
    read_at     DATETIME,
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL,
    deleted_at  DATETIME
);

CREATE INDEX IF NOT EXISTS idx_notifications_company ON notifications(company_id);
CREATE INDEX IF NOT EXISTS idx_notifications_user ON notifications(user_id);
CREATE INDEX IF NOT EXISTS idx_notifications_type ON notifications(type);
CREATE INDEX IF NOT EXISTS idx_notifications_read ON notifications(read_at);

CREATE TABLE IF NOT EXISTS licenses (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    company_id       INTEGER NOT NULL,
    name             TEXT NOT NULL,
    vendor           TEXT NOT NULL DEFAULT '',
    category         TEXT NOT NULL DEFAULT '',
    license_key      TEXT NOT NULL DEFAULT '',
    total_seats      INTEGER NOT NULL DEFAULT 0,
    purchase_date    DATETIME,
    expiration_date  DATETIME,
    termination_date DATETIME,
    remark           TEXT NOT NULL DEFAULT '',
    created_at       DATETIME NOT NULL,
    updated_at       DATETIME NOT NULL,
    deleted_at       DATETIME
);

CREATE INDEX IF NOT EXISTS idx_licenses_company ON licenses(company_id);
CREATE INDEX IF NOT EXISTS idx_licenses_expiration ON licenses(expiration_date);

CREATE TABLE IF NOT EXISTS consumables (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    company_id   INTEGER NOT NULL,
    name         TEXT NOT NULL,
    spec         TEXT NOT NULL DEFAULT '',
    unit         TEXT NOT NULL DEFAULT '',
    stock        INTEGER NOT NULL DEFAULT 0,
    min_quantity INTEGER NOT NULL DEFAULT 0,
    remark       TEXT NOT NULL DEFAULT '',
    created_at   DATETIME NOT NULL,
    updated_at   DATETIME NOT NULL,
    deleted_at   DATETIME
);

CREATE INDEX IF NOT EXISTS idx_consumables_company ON consumables(company_id);

-- 流水追加式不可变：无 updated_at/软删除（operation_logs 先例）
CREATE TABLE IF NOT EXISTS consumable_txns (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    company_id    INTEGER NOT NULL,
    consumable_id INTEGER NOT NULL,
    type          TEXT NOT NULL,
    delta         INTEGER NOT NULL,
    recipient     TEXT NOT NULL DEFAULT '',
    operator_id   INTEGER NOT NULL DEFAULT 0,
    operator_name TEXT NOT NULL,
    remark        TEXT NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_consumable_txns_company ON consumable_txns(company_id);
CREATE INDEX IF NOT EXISTS idx_consumable_txns_consumable ON consumable_txns(consumable_id);
CREATE INDEX IF NOT EXISTS idx_consumable_txns_type ON consumable_txns(type);
`

type SQLiteStore struct {
	db *sql.DB
}

func OpenSQLite(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

func newToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *SQLiteStore) RegisterDevice(ctx context.Context, d Device) (string, error) {
	if d.DeviceID == "" {
		return "", fmt.Errorf("device_id required")
	}
	for {
		token, err := newToken()
		if err != nil {
			return "", err
		}
		now := time.Now().UTC()
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO devices (device_id, device_token, hostname, os, agent_version, last_seen_at, registered_at)
			 VALUES (?,?,?,?,?,?,?)
			 ON CONFLICT(device_id) DO NOTHING`,
			d.DeviceID, token, d.Hostname, d.OS, d.AgentVersion, now, now)
		if err != nil {
			return "", err
		}
		var existing string
		err = s.db.QueryRowContext(ctx,
			`SELECT device_token FROM devices WHERE device_id = ?`, d.DeviceID).Scan(&existing)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return "", err
		}
		return existing, nil
	}
}

func (s *SQLiteStore) Authenticate(ctx context.Context, deviceID, token string) error {
	var stored string
	err := s.db.QueryRowContext(ctx,
		`SELECT device_token FROM devices WHERE device_id = ?`, deviceID).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUnauthorized
	}
	if err != nil {
		return err
	}
	if stored != token {
		return ErrUnauthorized
	}
	return nil
}

func (s *SQLiteStore) UpsertDeviceSeen(ctx context.Context, d Device) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE devices SET hostname=?, os=?, agent_version=?, last_seen_at=? WHERE device_id=?`,
		d.Hostname, d.OS, d.AgentVersion, time.Now().UTC(), d.DeviceID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) SaveReport(ctx context.Context, r Report) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO reports (device_id, report_type, payload, reported_at, received_at)
		 VALUES (?,?,?,?,?)`,
		r.DeviceID, r.ReportType, r.Payload, r.ReportedAt.UTC(), time.Now().UTC())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func scanDevice(row interface{ Scan(...any) error }) (Device, error) {
	var d Device
	err := row.Scan(&d.DeviceID, &d.Hostname, &d.OS, &d.AgentVersion, &d.LastSeenAt, &d.RegisteredAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	return d, err
}

func (s *SQLiteStore) GetDevice(ctx context.Context, deviceID string) (Device, error) {
	return scanDevice(s.db.QueryRowContext(ctx,
		`SELECT device_id, hostname, os, agent_version, last_seen_at, registered_at
		 FROM devices WHERE device_id = ?`, deviceID))
}

func (s *SQLiteStore) ListDevices(ctx context.Context, limit, offset int) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT device_id, hostname, os, agent_version, last_seen_at, registered_at
		 FROM devices ORDER BY registered_at LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetSnapshot(ctx context.Context, deviceID string) (Snapshot, error) {
	var snap Snapshot
	err := s.db.QueryRowContext(ctx,
		`SELECT device_id, payload, updated_at FROM snapshots WHERE device_id = ?`, deviceID).
		Scan(&snap.DeviceID, &snap.Payload, &snap.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Snapshot{}, ErrNotFound
	}
	return snap, err
}

func (s *SQLiteStore) SaveSnapshot(ctx context.Context, snap Snapshot) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO snapshots (device_id, payload, updated_at) VALUES (?,?,?)
		 ON CONFLICT(device_id) DO UPDATE SET payload=excluded.payload, updated_at=excluded.updated_at`,
		snap.DeviceID, snap.Payload, time.Now().UTC())
	return err
}

func (s *SQLiteStore) SaveChangeEvent(ctx context.Context, e ChangeEvent) (int64, error) {
	detail, _ := json.Marshal(e.Detail)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO change_events (device_id, kind, severity, message, detail, acked, created_at)
		 VALUES (?,?,?,?,?,0,?)`,
		e.DeviceID, e.Kind, e.Severity, e.Message, detail, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *SQLiteStore) ListChangeEvents(ctx context.Context, includeAcked bool, limit, offset int) ([]ChangeEvent, error) {
	query := `SELECT id, device_id, kind, severity, message, detail, acked, created_at FROM change_events`
	if !includeAcked {
		query += ` WHERE acked = 0`
	}
	query += ` ORDER BY id DESC LIMIT ? OFFSET ?`
	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChangeEvent
	for rows.Next() {
		var e ChangeEvent
		var detail []byte
		var acked int
		if err := rows.Scan(&e.ID, &e.DeviceID, &e.Kind, &e.Severity, &e.Message, &detail, &acked, &e.CreatedAt); err != nil {
			return nil, err
		}
		if len(detail) > 0 {
			_ = json.Unmarshal(detail, &e.Detail)
		}
		e.Acked = acked == 1
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) AckChangeEvent(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE change_events SET acked = 1 WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) ListReports(ctx context.Context, deviceID string, limit, offset int) ([]Report, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, device_id, report_type, payload, reported_at, received_at
		 FROM reports WHERE device_id = ? ORDER BY id DESC LIMIT ? OFFSET ?`,
		deviceID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Report
	for rows.Next() {
		var r Report
		if err := rows.Scan(&r.ID, &r.DeviceID, &r.ReportType, &r.Payload, &r.ReportedAt, &r.ReceivedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetProtectionModule(ctx context.Context, key string) (model.ProtectionModule, error) {
	var m model.ProtectionModule
	var enabled int
	err := s.db.QueryRowContext(ctx,
		`SELECT enabled, password_hash, updated_at FROM protection_modules WHERE module_key = ?`, key).
		Scan(&enabled, &m.PasswordHash, &m.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.ProtectionModule{ModuleKey: key}, nil
	}
	if err != nil {
		return model.ProtectionModule{}, fmt.Errorf("get protection module %s: %w", key, err)
	}
	m.ModuleKey = key
	m.Enabled = enabled == 1
	return m, nil
}

func (s *SQLiteStore) PutProtectionModule(ctx context.Context, m model.ProtectionModule) error {
	if m.ModuleKey != model.ProtectionModuleQuit && m.ModuleKey != model.ProtectionModuleUninstall {
		return fmt.Errorf("unknown protection module: %s", m.ModuleKey)
	}
	existing, err := s.GetProtectionModule(ctx, m.ModuleKey)
	if err != nil {
		return err
	}
	if m.PasswordHash == "" {
		m.PasswordHash = existing.PasswordHash
	}
	m.UpdatedAt = time.Now().UTC()
	enabled := 0
	if m.Enabled {
		enabled = 1
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO protection_modules (module_key, enabled, password_hash, updated_at) VALUES (?,?,?,?)
		 ON CONFLICT(module_key) DO UPDATE SET enabled=excluded.enabled, password_hash=excluded.password_hash, updated_at=excluded.updated_at`,
		m.ModuleKey, enabled, m.PasswordHash, m.UpdatedAt)
	if err != nil {
		return fmt.Errorf("put protection module %s: %w", m.ModuleKey, err)
	}
	return nil
}

func (s *SQLiteStore) CreateUninstallCode(ctx context.Context, deviceID string, ttl time.Duration) (model.UninstallCode, error) {
	if deviceID == "" {
		return model.UninstallCode{}, fmt.Errorf("device_id required")
	}
	n, err := rand.Int(rand.Reader, big.NewInt(100000000))
	if err != nil {
		return model.UninstallCode{}, fmt.Errorf("generate uninstall code: %w", err)
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO uninstall_codes (code, device_id, expires_at, created_at, updated_at) VALUES (?,?,?,?,?)`,
		fmt.Sprintf("%08d", n.Int64()), deviceID, now.Add(ttl), now, now)
	if err != nil {
		return model.UninstallCode{}, fmt.Errorf("create uninstall code: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.UninstallCode{}, fmt.Errorf("uninstall code id: %w", err)
	}
	return s.getUninstallCodeByID(ctx, id)
}

func (s *SQLiteStore) getUninstallCodeByID(ctx context.Context, id int64) (model.UninstallCode, error) {
	var uc model.UninstallCode
	var usedAt sql.NullTime
	err := s.db.QueryRowContext(ctx,
		`SELECT id, code, device_id, expires_at, used_at FROM uninstall_codes WHERE id = ?`, id).
		Scan(&uc.ID, &uc.Code, &uc.DeviceID, &uc.ExpiresAt, &usedAt)
	if err != nil {
		return model.UninstallCode{}, fmt.Errorf("load uninstall code: %w", err)
	}
	if usedAt.Valid {
		uc.UsedAt = &usedAt.Time
	}
	return uc, nil
}

func (s *SQLiteStore) VerifyUninstallCode(ctx context.Context, deviceID, code string) error {
	if deviceID == "" || code == "" {
		return ErrUnauthorized
	}
	var (
		id       int64
		expires  time.Time
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, expires_at FROM uninstall_codes
		 WHERE device_id = ? AND code = ? AND used_at IS NULL ORDER BY id DESC`, deviceID, code).
		Scan(&id, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUnauthorized
	}
	if err != nil {
		return fmt.Errorf("verify uninstall code: %w", err)
	}
	if time.Now().UTC().After(expires) {
		return ErrUnauthorized
	}
	res, err := s.db.ExecContext(ctx, `UPDATE uninstall_codes SET used_at = ? WHERE id = ? AND used_at IS NULL`,
		time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("mark uninstall code used: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrUnauthorized
	}
	return nil
}

func sqliteBool(b bool) int {
	if b {
		return 1
	}
	return 0
}

const dispatchColumns = `id, company_id, asset_id, borrower_name, destination, dispatched_at, expected_return_at, returned_at, isolation_offline, expect_wipe, status, remark, created_at, updated_at`

func scanDispatchRow(row interface{ Scan(...any) error }) (model.AssetDispatch, error) {
	var (
		d          model.AssetDispatch
		returnedAt sql.NullTime
	)
	err := row.Scan(&d.ID, &d.CompanyID, &d.AssetID, &d.BorrowerName, &d.Destination,
		&d.DispatchedAt, &d.ExpectedReturnAt, &returnedAt,
		&d.IsolationOffline, &d.ExpectWipe, &d.Status, &d.Remark, &d.CreatedAt, &d.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.AssetDispatch{}, ErrNotFound
	}
	if err != nil {
		return model.AssetDispatch{}, err
	}
	if returnedAt.Valid {
		d.ReturnedAt = &returnedAt.Time
	}
	return d, nil
}

func (s *SQLiteStore) CreateDispatch(ctx context.Context, d model.AssetDispatch) (model.AssetDispatch, error) {
	if err := validateDispatch(d); err != nil {
		return model.AssetDispatch{}, err
	}
	if d.Status == 0 {
		d.Status = model.DispatchStatusActive
	}
	if d.Status != model.DispatchStatusActive {
		return model.AssetDispatch{}, fmt.Errorf("new dispatch must be in active status")
	}
	if d.DispatchedAt.IsZero() {
		d.DispatchedAt = time.Now().UTC()
	}

	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AssetDispatch{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // 提交后回滚是无害空操作

	var count int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM asset_dispatches
		 WHERE company_id = ? AND asset_id = ? AND status = ? AND deleted_at IS NULL`,
		d.CompanyID, d.AssetID, model.DispatchStatusActive).Scan(&count); err != nil {
		return model.AssetDispatch{}, fmt.Errorf("count active dispatches: %w", err)
	}
	if count > 0 {
		return model.AssetDispatch{}, ErrAlreadyExists
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO asset_dispatches
		 (company_id, asset_id, borrower_name, destination, dispatched_at, expected_return_at,
		  isolation_offline, expect_wipe, status, remark, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.CompanyID, d.AssetID, d.BorrowerName, d.Destination,
		d.DispatchedAt.UTC(), d.ExpectedReturnAt.UTC(),
		sqliteBool(d.IsolationOffline), sqliteBool(d.ExpectWipe),
		d.Status, d.Remark, now, now)
	if err != nil {
		return model.AssetDispatch{}, fmt.Errorf("insert dispatch: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.AssetDispatch{}, fmt.Errorf("dispatch id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return model.AssetDispatch{}, fmt.Errorf("commit dispatch: %w", err)
	}
	d.ID = id
	d.CreatedAt, d.UpdatedAt = now, now
	return d, nil
}

func (s *SQLiteStore) ListDispatches(ctx context.Context, f DispatchListFilter) ([]model.AssetDispatch, int64, error) {
	page, size := normalizeDispatchPage(f.Page, f.PageSize)

	where := "deleted_at IS NULL"
	args := []any{}
	if f.CompanyID > 0 {
		where += " AND company_id = ?"
		args = append(args, f.CompanyID)
	}
	if f.AssetID > 0 {
		where += " AND asset_id = ?"
		args = append(args, f.AssetID)
	}
	if f.Status > 0 {
		where += " AND status = ?"
		args = append(args, f.Status)
	}
	if f.Overdue != nil {
		if *f.Overdue {
			where += " AND status = ? AND expected_return_at < ?"
		} else {
			where += " AND NOT (status = ? AND expected_return_at < ?)"
		}
		args = append(args, model.DispatchStatusActive, time.Now().UTC())
	}

	var total int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM asset_dispatches WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count dispatches: %w", err)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+dispatchColumns+` FROM asset_dispatches WHERE `+where+
			` ORDER BY id DESC LIMIT ? OFFSET ?`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list dispatches: %w", err)
	}
	defer rows.Close()

	items := make([]model.AssetDispatch, 0, size)
	for rows.Next() {
		d, err := scanDispatchRow(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, d)
	}
	return items, total, rows.Err()
}

// transitionDispatchStatus 条件更新：仅"外派中"可流转；未命中时区分
// "不存在或跨公司"与"状态不允许"两类失败，与 GormStore 语义保持一致
func (s *SQLiteStore) transitionDispatchStatus(ctx context.Context, companyID, id int64, toStatus int, returnedAt *time.Time) (model.AssetDispatch, error) {
	query := `UPDATE asset_dispatches SET status = ?, updated_at = ?`
	args := []any{toStatus, time.Now().UTC()}
	if returnedAt != nil {
		query += `, returned_at = ?`
		args = append(args, returnedAt.UTC())
	}
	query += ` WHERE id = ? AND company_id = ? AND status = ? AND deleted_at IS NULL`
	args = append(args, id, companyID, model.DispatchStatusActive)

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return model.AssetDispatch{}, fmt.Errorf("update dispatch status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return model.AssetDispatch{}, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		var exists int
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM asset_dispatches WHERE id = ? AND company_id = ? AND deleted_at IS NULL`,
			id, companyID).Scan(&exists); err != nil {
			return model.AssetDispatch{}, fmt.Errorf("check dispatch exists: %w", err)
		}
		if exists == 0 {
			return model.AssetDispatch{}, ErrNotFound
		}
		return model.AssetDispatch{}, ErrInvalidState
	}
	return s.getDispatchByID(ctx, id)
}

func (s *SQLiteStore) getDispatchByID(ctx context.Context, id int64) (model.AssetDispatch, error) {
	return scanDispatchRow(s.db.QueryRowContext(ctx,
		`SELECT `+dispatchColumns+` FROM asset_dispatches WHERE id = ? AND deleted_at IS NULL`, id))
}

func (s *SQLiteStore) ReturnDispatch(ctx context.Context, companyID, id int64, returnedAt time.Time) (model.AssetDispatch, error) {
	return s.transitionDispatchStatus(ctx, companyID, id, model.DispatchStatusReturned, &returnedAt)
}

func (s *SQLiteStore) CancelDispatch(ctx context.Context, companyID, id int64) (model.AssetDispatch, error) {
	return s.transitionDispatchStatus(ctx, companyID, id, model.DispatchStatusCanceled, nil)
}

func (s *SQLiteStore) GetActiveDispatchByAsset(ctx context.Context, companyID, assetID int64) (model.AssetDispatch, error) {
	return scanDispatchRow(s.db.QueryRowContext(ctx,
		`SELECT `+dispatchColumns+` FROM asset_dispatches
		 WHERE company_id = ? AND asset_id = ? AND status = ? AND deleted_at IS NULL`,
		companyID, assetID, model.DispatchStatusActive))
}

func (s *SQLiteStore) GetWebhookAlertConfig(ctx context.Context) (model.WebhookAlertConfig, error) {
	var (
		cfg     model.WebhookAlertConfig
		enabled int
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, enabled, webhook_url, secret, cooldown_minutes, updated_at
		 FROM webhook_alert_config WHERE id = 1`).
		Scan(&cfg.ID, &enabled, &cfg.WebhookURL, &cfg.Secret, &cfg.CooldownMinutes, &cfg.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.WebhookAlertConfig{CooldownMinutes: model.DefaultWebhookCooldownMinutes}, nil
	}
	if err != nil {
		return model.WebhookAlertConfig{}, fmt.Errorf("get webhook alert config: %w", err)
	}
	cfg.Enabled = enabled == 1
	if cfg.CooldownMinutes <= 0 {
		cfg.CooldownMinutes = model.DefaultWebhookCooldownMinutes
	}
	return cfg, nil
}

func (s *SQLiteStore) PutWebhookAlertConfig(ctx context.Context, cfg model.WebhookAlertConfig) error {
	cfg.ID = 1
	cfg.UpdatedAt = time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO webhook_alert_config (id, enabled, webhook_url, secret, cooldown_minutes, updated_at)
		 VALUES (1, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET enabled=excluded.enabled, webhook_url=excluded.webhook_url,
		   secret=excluded.secret, cooldown_minutes=excluded.cooldown_minutes, updated_at=excluded.updated_at`,
		sqliteBool(cfg.Enabled), cfg.WebhookURL, cfg.Secret, cfg.CooldownMinutes, cfg.UpdatedAt)
	if err != nil {
		return fmt.Errorf("put webhook alert config: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetAgentSettings(ctx context.Context) (model.AgentSettings, error) {
	var cfg model.AgentSettings
	err := s.db.QueryRowContext(ctx,
		`SELECT id, heartbeat_interval_sec, full_interval_sec, offline_threshold_sec, updated_at
		 FROM agent_settings WHERE id = 1`).
		Scan(&cfg.ID, &cfg.HeartbeatIntervalSec, &cfg.FullIntervalSec,
			&cfg.OfflineThresholdSec, &cfg.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.DefaultAgentSettings(), nil
	}
	if err != nil {
		return model.AgentSettings{}, fmt.Errorf("get agent settings: %w", err)
	}
	return cfg, nil
}

func (s *SQLiteStore) PutAgentSettings(ctx context.Context, cfg model.AgentSettings) error {
	cfg.ID = model.AgentSettingsSingletonID
	cfg.UpdatedAt = time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO agent_settings (id, heartbeat_interval_sec, full_interval_sec, offline_threshold_sec, updated_at)
		 VALUES (1, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET heartbeat_interval_sec=excluded.heartbeat_interval_sec,
		   full_interval_sec=excluded.full_interval_sec, offline_threshold_sec=excluded.offline_threshold_sec,
		   updated_at=excluded.updated_at`,
		cfg.HeartbeatIntervalSec, cfg.FullIntervalSec, cfg.OfflineThresholdSec, cfg.UpdatedAt)
	if err != nil {
		return fmt.Errorf("put agent settings: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListWebhookAlertStates(ctx context.Context) ([]model.WebhookAlertState, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, company_id, asset_id, alert_type, sent_at FROM webhook_alert_states
		 WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list webhook alert states: %w", err)
	}
	defer rows.Close()
	states := []model.WebhookAlertState{}
	for rows.Next() {
		var st model.WebhookAlertState
		if err := rows.Scan(&st.ID, &st.CompanyID, &st.AssetID, &st.AlertType, &st.SentAt); err != nil {
			return nil, fmt.Errorf("scan webhook alert state: %w", err)
		}
		states = append(states, st)
	}
	return states, rows.Err()
}

func (s *SQLiteStore) PutWebhookAlertState(ctx context.Context, st model.WebhookAlertState) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO webhook_alert_states (company_id, asset_id, alert_type, sent_at, created_at, updated_at)
		 VALUES (?,?,?,?,?,?)
		 ON CONFLICT(company_id, asset_id, alert_type) DO UPDATE SET sent_at=excluded.sent_at, updated_at=excluded.updated_at`,
		st.CompanyID, st.AssetID, st.AlertType, st.SentAt.UTC(), now, now)
	if err != nil {
		return fmt.Errorf("put webhook alert state: %w", err)
	}
	return nil
}

// ---- 盘点任务（阶段五 P0-β）----

const stocktakeColumns = `id, company_id, name, remark, status, started_at, finished_at, created_by, scan_token_hash, created_at, updated_at`

func scanStocktakeRow(row interface{ Scan(...any) error }) (model.Stocktake, error) {
	var (
		st         model.Stocktake
		startedAt  sql.NullTime
		finishedAt sql.NullTime
		createdBy  sql.NullInt64
	)
	err := row.Scan(&st.ID, &st.CompanyID, &st.Name, &st.Remark, &st.Status,
		&startedAt, &finishedAt, &createdBy, &st.ScanTokenHash, &st.CreatedAt, &st.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Stocktake{}, ErrNotFound
	}
	if err != nil {
		return model.Stocktake{}, err
	}
	if startedAt.Valid {
		st.StartedAt = &startedAt.Time
	}
	if finishedAt.Valid {
		st.FinishedAt = &finishedAt.Time
	}
	if createdBy.Valid {
		id := createdBy.Int64
		st.CreatedBy = &id
	}
	return st, nil
}

const stocktakeItemColumns = `id, company_id, stocktake_id, asset_id, asset_tag, expected_location, expected_status, actual_location, result, scanned_by, scanned_at, remark, created_at, updated_at`

func scanStocktakeItemRow(row interface{ Scan(...any) error }) (model.StocktakeItem, error) {
	var (
		it        model.StocktakeItem
		scannedAt sql.NullTime
	)
	err := row.Scan(&it.ID, &it.CompanyID, &it.StocktakeID, &it.AssetID, &it.AssetTag,
		&it.ExpectedLocation, &it.ExpectedStatus, &it.ActualLocation, &it.Result,
		&it.ScannedBy, &scannedAt, &it.Remark, &it.CreatedAt, &it.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.StocktakeItem{}, ErrNotFound
	}
	if err != nil {
		return model.StocktakeItem{}, err
	}
	if scannedAt.Valid {
		it.ScannedAt = &scannedAt.Time
	}
	return it, nil
}

func (s *SQLiteStore) CreateStocktake(ctx context.Context, st model.Stocktake, items []model.StocktakeItem) (model.Stocktake, error) {
	if err := validateStocktake(st, items); err != nil {
		return model.Stocktake{}, err
	}
	st.Status = model.StocktakeStatusDraft

	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Stocktake{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // 提交后回滚是无害空操作

	res, err := tx.ExecContext(ctx,
		`INSERT INTO stocktakes (company_id, name, remark, status, created_by, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?)`,
		st.CompanyID, st.Name, st.Remark, st.Status, st.CreatedBy, now, now)
	if err != nil {
		return model.Stocktake{}, fmt.Errorf("insert stocktake: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.Stocktake{}, fmt.Errorf("stocktake id: %w", err)
	}
	for _, it := range items {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO stocktake_items
			 (company_id, stocktake_id, asset_id, asset_tag, expected_location, expected_status, result, created_at, updated_at)
			 VALUES (?,?,?,?,?,?,?,?,?)`,
			st.CompanyID, id, it.AssetID, it.AssetTag, it.ExpectedLocation, it.ExpectedStatus,
			model.StocktakeItemPending, now, now); err != nil {
			return model.Stocktake{}, fmt.Errorf("insert stocktake item: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return model.Stocktake{}, fmt.Errorf("commit stocktake: %w", err)
	}
	st.ID = id
	st.CreatedAt, st.UpdatedAt = now, now
	return st, nil
}

func (s *SQLiteStore) ListStocktakes(ctx context.Context, f StocktakeListFilter) ([]model.Stocktake, int64, error) {
	page, size := normalizeStocktakePage(f.Page, f.PageSize)

	where := "deleted_at IS NULL"
	args := []any{}
	if f.CompanyID > 0 {
		where += " AND company_id = ?"
		args = append(args, f.CompanyID)
	}
	if f.Status > 0 {
		where += " AND status = ?"
		args = append(args, f.Status)
	}

	var total int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM stocktakes WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count stocktakes: %w", err)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+stocktakeColumns+` FROM stocktakes WHERE `+where+
			` ORDER BY id DESC LIMIT ? OFFSET ?`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list stocktakes: %w", err)
	}
	defer rows.Close()

	items := make([]model.Stocktake, 0, size)
	for rows.Next() {
		st, err := scanStocktakeRow(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, st)
	}
	return items, total, rows.Err()
}

func (s *SQLiteStore) GetStocktake(ctx context.Context, companyID, id int64) (model.Stocktake, error) {
	return scanStocktakeRow(s.db.QueryRowContext(ctx,
		`SELECT `+stocktakeColumns+` FROM stocktakes WHERE id = ? AND company_id = ? AND deleted_at IS NULL`,
		id, companyID))
}

// transitionStocktake 条件更新驱动状态机，语义与 GormStore 保持一致：
// 未命中时区分"不存在或跨公司"与"状态不允许"
func (s *SQLiteStore) transitionStocktake(ctx context.Context, companyID, id int64, fromStatuses []int, toStatus int, extra map[string]any) (model.Stocktake, error) {
	query := `UPDATE stocktakes SET status = ?, updated_at = ?`
	args := []any{toStatus, time.Now().UTC()}
	for col, val := range extra {
		query += `, ` + col + ` = ?`
		args = append(args, val)
	}
	query += ` WHERE id = ? AND company_id = ? AND status IN (`
	args = append(args, id, companyID)
	for i, from := range fromStatuses {
		if i > 0 {
			query += `,`
		}
		query += `?`
		args = append(args, from)
	}
	query += `) AND deleted_at IS NULL`

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return model.Stocktake{}, fmt.Errorf("update stocktake status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return model.Stocktake{}, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		var exists int
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM stocktakes WHERE id = ? AND company_id = ? AND deleted_at IS NULL`,
			id, companyID).Scan(&exists); err != nil {
			return model.Stocktake{}, fmt.Errorf("check stocktake exists: %w", err)
		}
		if exists == 0 {
			return model.Stocktake{}, ErrNotFound
		}
		return model.Stocktake{}, ErrInvalidState
	}
	return s.getStocktakeByID(ctx, id)
}

func (s *SQLiteStore) getStocktakeByID(ctx context.Context, id int64) (model.Stocktake, error) {
	return scanStocktakeRow(s.db.QueryRowContext(ctx,
		`SELECT `+stocktakeColumns+` FROM stocktakes WHERE id = ? AND deleted_at IS NULL`, id))
}

func (s *SQLiteStore) StartStocktake(ctx context.Context, companyID, id int64) (model.Stocktake, string, error) {
	token, err := newScanToken()
	if err != nil {
		return model.Stocktake{}, "", fmt.Errorf("generate scan token: %w", err)
	}
	st, err := s.transitionStocktake(ctx, companyID, id,
		[]int{model.StocktakeStatusDraft}, model.StocktakeStatusProcessing,
		map[string]any{"started_at": time.Now().UTC(), "scan_token_hash": hashScanToken(token)})
	return st, token, err
}

func (s *SQLiteStore) RotateStocktakeToken(ctx context.Context, companyID, id int64) (string, error) {
	token, err := newScanToken()
	if err != nil {
		return "", fmt.Errorf("generate scan token: %w", err)
	}
	_, err = s.transitionStocktake(ctx, companyID, id,
		[]int{model.StocktakeStatusProcessing}, model.StocktakeStatusProcessing,
		map[string]any{"scan_token_hash": hashScanToken(token)})
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *SQLiteStore) FinishStocktake(ctx context.Context, companyID, id int64, finishedAt time.Time) (model.Stocktake, error) {
	return s.transitionStocktake(ctx, companyID, id,
		[]int{model.StocktakeStatusProcessing}, model.StocktakeStatusFinished,
		map[string]any{"finished_at": finishedAt})
}

func (s *SQLiteStore) CancelStocktake(ctx context.Context, companyID, id int64) (model.Stocktake, error) {
	return s.transitionStocktake(ctx, companyID, id,
		[]int{model.StocktakeStatusDraft, model.StocktakeStatusProcessing}, model.StocktakeStatusCanceled, nil)
}

func (s *SQLiteStore) GetStocktakeByToken(ctx context.Context, token string) (model.Stocktake, error) {
	if token == "" {
		return model.Stocktake{}, ErrNotFound
	}
	return scanStocktakeRow(s.db.QueryRowContext(ctx,
		`SELECT `+stocktakeColumns+` FROM stocktakes
		 WHERE scan_token_hash = ? AND status = ? AND deleted_at IS NULL`,
		hashScanToken(token), model.StocktakeStatusProcessing))
}

func (s *SQLiteStore) ListStocktakeItems(ctx context.Context, f StocktakeItemListFilter) ([]model.StocktakeItem, int64, error) {
	page, size := normalizeStocktakePage(f.Page, f.PageSize)

	where := "deleted_at IS NULL"
	args := []any{}
	if f.CompanyID > 0 {
		where += " AND company_id = ?"
		args = append(args, f.CompanyID)
	}
	if f.StocktakeID > 0 {
		where += " AND stocktake_id = ?"
		args = append(args, f.StocktakeID)
	}
	if f.Result > 0 {
		where += " AND result = ?"
		args = append(args, f.Result)
	}
	if f.Keyword != "" {
		where += " AND asset_tag LIKE ?"
		args = append(args, "%"+f.Keyword+"%")
	}

	var total int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM stocktake_items WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count stocktake items: %w", err)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+stocktakeItemColumns+` FROM stocktake_items WHERE `+where+
			` ORDER BY id ASC LIMIT ? OFFSET ?`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list stocktake items: %w", err)
	}
	defer rows.Close()

	items := make([]model.StocktakeItem, 0, size)
	for rows.Next() {
		it, err := scanStocktakeItemRow(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, it)
	}
	return items, total, rows.Err()
}

func (s *SQLiteStore) CheckStocktakeItems(ctx context.Context, companyID, stocktakeID int64, checks []model.StocktakeCheck, scannedBy string) ([]model.StocktakeItem, error) {
	if err := validateStocktakeChecks(checks); err != nil {
		return nil, err
	}
	if scannedBy == "" {
		return nil, fmt.Errorf("scanned_by required")
	}
	st, err := s.GetStocktake(ctx, companyID, stocktakeID)
	if err != nil {
		return nil, err
	}
	if st.Status != model.StocktakeStatusProcessing {
		return nil, ErrInvalidState
	}

	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // 提交后回滚是无害空操作

	for i, ck := range checks {
		query := `UPDATE stocktake_items SET result = ?, scanned_by = ?, scanned_at = ?, updated_at = ?`
		args := []any{ck.Result, scannedBy, now, now}
		if ck.ActualLocation != "" {
			query += `, actual_location = ?`
			args = append(args, ck.ActualLocation)
		}
		if ck.Remark != "" {
			query += `, remark = ?`
			args = append(args, ck.Remark)
		}
		query += ` WHERE stocktake_id = ? AND company_id = ?`
		args = append(args, stocktakeID, companyID)
		if ck.ItemID > 0 {
			query += ` AND id = ?`
			args = append(args, ck.ItemID)
		} else {
			query += ` AND asset_tag = ?`
			args = append(args, ck.AssetTag)
		}
		query += ` AND deleted_at IS NULL`

		res, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("update item %d: %w", i, err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil, ErrNotFound
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit checks: %w", err)
	}

	// 回读更新后的明细，按提交顺序排列
	out := make([]model.StocktakeItem, 0, len(checks))
	for _, ck := range checks {
		var (
			it  model.StocktakeItem
			err error
		)
		if ck.ItemID > 0 {
			it, err = s.getStocktakeItemByID(ctx, stocktakeID, companyID, ck.ItemID)
		} else {
			it, err = s.getStocktakeItemByTag(ctx, stocktakeID, companyID, ck.AssetTag)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, nil
}

func (s *SQLiteStore) getStocktakeItemByID(ctx context.Context, stocktakeID, companyID, id int64) (model.StocktakeItem, error) {
	return scanStocktakeItemRow(s.db.QueryRowContext(ctx,
		`SELECT `+stocktakeItemColumns+` FROM stocktake_items
		 WHERE id = ? AND stocktake_id = ? AND company_id = ? AND deleted_at IS NULL`,
		id, stocktakeID, companyID))
}

func (s *SQLiteStore) getStocktakeItemByTag(ctx context.Context, stocktakeID, companyID int64, tag string) (model.StocktakeItem, error) {
	return scanStocktakeItemRow(s.db.QueryRowContext(ctx,
		`SELECT `+stocktakeItemColumns+` FROM stocktake_items
		 WHERE asset_tag = ? AND stocktake_id = ? AND company_id = ? AND deleted_at IS NULL`,
		tag, stocktakeID, companyID))
}

func (s *SQLiteStore) CountStocktakeResults(ctx context.Context, companyID, stocktakeID int64) (map[int]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT result, COUNT(*) FROM stocktake_items
		 WHERE company_id = ? AND stocktake_id = ? AND deleted_at IS NULL GROUP BY result`,
		companyID, stocktakeID)
	if err != nil {
		return nil, fmt.Errorf("count stocktake results: %w", err)
	}
	defer rows.Close()

	counts := map[int]int64{
		model.StocktakeItemPending:  0,
		model.StocktakeItemNormal:   0,
		model.StocktakeItemLost:     0,
		model.StocktakeItemDamaged:  0,
		model.StocktakeItemScrapped: 0,
	}
	for rows.Next() {
		var result, n int64
		if err := rows.Scan(&result, &n); err != nil {
			return nil, fmt.Errorf("scan result count: %w", err)
		}
		counts[int(result)] = n
	}
	return counts, rows.Err()
}

// ---- 设备申请（阶段五 P0-β）----

const assetRequestColumns = `id, company_id, asset_id, applicant_id, applicant_name, is_long_term, expected_return_at, reason, status, approved_by, approved_at, decision_remark, created_at, updated_at`

func scanAssetRequestRow(row interface{ Scan(...any) error }) (model.AssetRequest, error) {
	var (
		r          model.AssetRequest
		isLongTerm int
		returnAt   sql.NullTime
		approvedBy sql.NullInt64
		approvedAt sql.NullTime
	)
	err := row.Scan(&r.ID, &r.CompanyID, &r.AssetID, &r.ApplicantID, &r.ApplicantName,
		&isLongTerm, &returnAt, &r.Reason, &r.Status,
		&approvedBy, &approvedAt, &r.DecisionRemark, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.AssetRequest{}, ErrNotFound
	}
	if err != nil {
		return model.AssetRequest{}, err
	}
	r.IsLongTerm = isLongTerm == 1
	if returnAt.Valid {
		t := returnAt.Time
		r.ExpectedReturnAt = &t
	}
	if approvedBy.Valid {
		id := approvedBy.Int64
		r.ApprovedBy = &id
	}
	if approvedAt.Valid {
		t := approvedAt.Time
		r.ApprovedAt = &t
	}
	return r, nil
}

func (s *SQLiteStore) CreateAssetRequest(ctx context.Context, r model.AssetRequest) (model.AssetRequest, error) {
	r, err := validateAssetRequest(r)
	if err != nil {
		return model.AssetRequest{}, err
	}
	r.Status = model.AssetRequestStatusPending

	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AssetRequest{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // 提交后回滚是无害空操作

	var count int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM asset_requests
		 WHERE company_id = ? AND asset_id = ? AND applicant_id = ? AND status = ? AND deleted_at IS NULL`,
		r.CompanyID, r.AssetID, r.ApplicantID, model.AssetRequestStatusPending).Scan(&count); err != nil {
		return model.AssetRequest{}, fmt.Errorf("count pending requests: %w", err)
	}
	if count > 0 {
		return model.AssetRequest{}, ErrAlreadyExists
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO asset_requests
		 (company_id, asset_id, applicant_id, applicant_name, is_long_term, expected_return_at, reason, status, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		r.CompanyID, r.AssetID, r.ApplicantID, r.ApplicantName,
		sqliteBool(r.IsLongTerm), r.ExpectedReturnAt, r.Reason, r.Status, now, now)
	if err != nil {
		return model.AssetRequest{}, fmt.Errorf("insert asset request: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.AssetRequest{}, fmt.Errorf("asset request id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return model.AssetRequest{}, fmt.Errorf("commit asset request: %w", err)
	}
	r.ID = id
	r.CreatedAt, r.UpdatedAt = now, now
	return r, nil
}

func (s *SQLiteStore) ListAssetRequests(ctx context.Context, f AssetRequestListFilter) ([]model.AssetRequest, int64, error) {
	page, size := normalizeAssetRequestPage(f.Page, f.PageSize)

	where := "deleted_at IS NULL"
	args := []any{}
	if f.CompanyID > 0 {
		where += " AND company_id = ?"
		args = append(args, f.CompanyID)
	}
	if f.Status > 0 {
		where += " AND status = ?"
		args = append(args, f.Status)
	}
	if f.ApplicantID > 0 {
		where += " AND applicant_id = ?"
		args = append(args, f.ApplicantID)
	}
	if f.AssetID > 0 {
		where += " AND asset_id = ?"
		args = append(args, f.AssetID)
	}

	var total int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM asset_requests WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count asset requests: %w", err)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+assetRequestColumns+` FROM asset_requests WHERE `+where+
			` ORDER BY id DESC LIMIT ? OFFSET ?`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list asset requests: %w", err)
	}
	defer rows.Close()

	items := make([]model.AssetRequest, 0, size)
	for rows.Next() {
		r, err := scanAssetRequestRow(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, r)
	}
	return items, total, rows.Err()
}

func (s *SQLiteStore) GetAssetRequest(ctx context.Context, companyID, id int64) (model.AssetRequest, error) {
	return scanAssetRequestRow(s.db.QueryRowContext(ctx,
		`SELECT `+assetRequestColumns+` FROM asset_requests
		 WHERE id = ? AND company_id = ? AND deleted_at IS NULL`, id, companyID))
}

// transitionAssetRequest 条件更新：仅待审批可流转，语义与 GormStore 一致
func (s *SQLiteStore) transitionAssetRequest(ctx context.Context, companyID, id int64, toStatus int, extra map[string]any) (model.AssetRequest, error) {
	query := `UPDATE asset_requests SET status = ?`
	args := []any{toStatus}
	for col, val := range extra {
		query += `, ` + col + ` = ?`
		args = append(args, val)
	}
	query += `, updated_at = ?`
	args = append(args, time.Now().UTC())
	query += ` WHERE id = ? AND company_id = ? AND status = ? AND deleted_at IS NULL`
	args = append(args, id, companyID, model.AssetRequestStatusPending)

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return model.AssetRequest{}, fmt.Errorf("update asset request status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return model.AssetRequest{}, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		var exists int
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM asset_requests WHERE id = ? AND company_id = ? AND deleted_at IS NULL`,
			id, companyID).Scan(&exists); err != nil {
			return model.AssetRequest{}, fmt.Errorf("check asset request exists: %w", err)
		}
		if exists == 0 {
			return model.AssetRequest{}, ErrNotFound
		}
		return model.AssetRequest{}, ErrInvalidState
	}
	return s.getAssetRequestByID(ctx, id)
}

func (s *SQLiteStore) getAssetRequestByID(ctx context.Context, id int64) (model.AssetRequest, error) {
	return scanAssetRequestRow(s.db.QueryRowContext(ctx,
		`SELECT `+assetRequestColumns+` FROM asset_requests WHERE id = ? AND deleted_at IS NULL`, id))
}

func (s *SQLiteStore) RejectAssetRequest(ctx context.Context, companyID, id int64, approverID int64, remark string, now time.Time) (model.AssetRequest, error) {
	return s.transitionAssetRequest(ctx, companyID, id, model.AssetRequestStatusRejected, map[string]any{
		"approved_by":     approverID,
		"approved_at":     now,
		"decision_remark": remark,
	})
}

func (s *SQLiteStore) CancelAssetRequest(ctx context.Context, companyID, id int64) (model.AssetRequest, error) {
	return s.transitionAssetRequest(ctx, companyID, id, model.AssetRequestStatusCanceled, nil)
}

// ---- 折旧规则（阶段五 P0-β）----

const depreciationRuleColumns = `id, company_id, name, months, floor_type, floor_val, stages, enabled, remark, created_at, updated_at`

func scanDepreciationRuleRow(row interface{ Scan(...any) error }) (model.DepreciationRule, error) {
	var (
		r       model.DepreciationRule
		enabled int
	)
	err := row.Scan(&r.ID, &r.CompanyID, &r.Name, &r.Months, &r.FloorType, &r.FloorVal,
		&r.Stages, &enabled, &r.Remark, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.DepreciationRule{}, ErrNotFound
	}
	if err != nil {
		return model.DepreciationRule{}, err
	}
	r.Enabled = enabled == 1
	return r, nil
}

func (s *SQLiteStore) CreateDepreciationRule(ctx context.Context, r model.DepreciationRule) (model.DepreciationRule, error) {
	if err := validateDepreciationRule(r); err != nil {
		return model.DepreciationRule{}, err
	}
	if r.FloorType == "" {
		r.FloorType = "percent"
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO depreciations
		 (company_id, name, months, floor_type, floor_val, stages, enabled, remark, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		r.CompanyID, r.Name, r.Months, r.FloorType, r.FloorVal, r.Stages,
		sqliteBool(r.Enabled), r.Remark, now, now)
	if err != nil {
		return model.DepreciationRule{}, fmt.Errorf("insert depreciation rule: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.DepreciationRule{}, fmt.Errorf("depreciation rule id: %w", err)
	}
	r.ID = id
	r.CreatedAt, r.UpdatedAt = now, now
	return r, nil
}

func (s *SQLiteStore) ListDepreciationRules(ctx context.Context, f DepreciationRuleListFilter) ([]model.DepreciationRule, int64, error) {
	page, size := normalizeDispatchPage(f.Page, f.PageSize)

	var total int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM depreciations WHERE company_id = ? AND deleted_at IS NULL`,
		f.CompanyID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count depreciation rules: %w", err)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+depreciationRuleColumns+` FROM depreciations
		 WHERE company_id = ? AND deleted_at IS NULL ORDER BY id DESC LIMIT ? OFFSET ?`,
		f.CompanyID, size, (page-1)*size)
	if err != nil {
		return nil, 0, fmt.Errorf("list depreciation rules: %w", err)
	}
	defer rows.Close()

	items := make([]model.DepreciationRule, 0, size)
	for rows.Next() {
		r, err := scanDepreciationRuleRow(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, r)
	}
	return items, total, rows.Err()
}

func (s *SQLiteStore) GetDepreciationRule(ctx context.Context, companyID, id int64) (model.DepreciationRule, error) {
	return scanDepreciationRuleRow(s.db.QueryRowContext(ctx,
		`SELECT `+depreciationRuleColumns+` FROM depreciations
		 WHERE id = ? AND company_id = ? AND deleted_at IS NULL`, id, companyID))
}

func (s *SQLiteStore) UpdateDepreciationRule(ctx context.Context, r model.DepreciationRule) (model.DepreciationRule, error) {
	if err := validateDepreciationRule(r); err != nil {
		return model.DepreciationRule{}, err
	}
	if r.ID == 0 {
		return model.DepreciationRule{}, fmt.Errorf("id required")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE depreciations SET name = ?, months = ?, floor_type = ?, floor_val = ?,
		 stages = ?, enabled = ?, remark = ?, updated_at = ?
		 WHERE id = ? AND company_id = ? AND deleted_at IS NULL`,
		r.Name, r.Months, r.FloorType, r.FloorVal, r.Stages,
		sqliteBool(r.Enabled), r.Remark, time.Now().UTC(), r.ID, r.CompanyID)
	if err != nil {
		return model.DepreciationRule{}, fmt.Errorf("update depreciation rule: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return model.DepreciationRule{}, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return model.DepreciationRule{}, ErrNotFound
	}
	return s.GetDepreciationRule(ctx, r.CompanyID, r.ID)
}

func (s *SQLiteStore) DeleteDepreciationRule(ctx context.Context, companyID, id int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE depreciations SET deleted_at = ?, updated_at = ?
		 WHERE id = ? AND company_id = ? AND deleted_at IS NULL`,
		time.Now().UTC(), time.Now().UTC(), id, companyID)
	if err != nil {
		return fmt.Errorf("delete depreciation rule: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ==================== 维度治理（P1）：厂商/供应商/位置/型号库 ====================
// 与 GormStore 同一套契约：名称 trim 落库、同公司唯一（软删除不占名）、
// 公司边界（跨公司一律 NotFound）、keyword 模糊 + 分页倒序。
// 维表被资产/型号/子位置引用的删除拦截在 API 层（SQLiteStore 测试库无 assets 表）

// dimensionNameTaken 名称占用检查；table 为编译期字面量（禁止拼接用户输入），
// id > 0 时排除自身（更新保留原名不算冲突）
func (s *SQLiteStore) dimensionNameTaken(ctx context.Context, table string, companyID, id int64, name string) (bool, error) {
	q := `SELECT COUNT(*) FROM ` + table + ` WHERE company_id = ? AND name = ? AND deleted_at IS NULL`
	args := []any{companyID, name}
	if id > 0 {
		q += ` AND id <> ?`
		args = append(args, id)
	}
	var cnt int64
	if err := s.db.QueryRowContext(ctx, q, args...).Scan(&cnt); err != nil {
		return false, err
	}
	return cnt > 0, nil
}

// deleteDimension 软删除维表记录；幂等语义：不存在/跨公司一律 NotFound
func (s *SQLiteStore) deleteDimension(ctx context.Context, table, what string, companyID, id int64) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`UPDATE `+table+` SET deleted_at = ?, updated_at = ?
		 WHERE id = ? AND company_id = ? AND deleted_at IS NULL`,
		now, now, id, companyID)
	if err != nil {
		return fmt.Errorf("delete %s: %w", what, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// dimensionWhere 维表列表 WHERE 片段（公司边界 + 可选名称模糊），
// 返回占位符条件与参数（keyword 追加时参数顺序与占位符一致）
func dimensionWhere(companyID int64, keyword string) (string, []any) {
	where := `company_id = ? AND deleted_at IS NULL`
	args := []any{companyID}
	if kw := strings.TrimSpace(keyword); kw != "" {
		where += ` AND name LIKE ?`
		args = append(args, "%"+kw+"%")
	}
	return where, args
}

// nullableInt64 *int64 → SQL 可空参数（nil 转 NULL）
func nullableInt64(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

// ---- 厂商 ----

const manufacturerColumns = `id, company_id, name, remark, created_at, updated_at`

func scanManufacturerRow(row interface{ Scan(...any) error }) (model.Manufacturer, error) {
	var m model.Manufacturer
	err := row.Scan(&m.ID, &m.CompanyID, &m.Name, &m.Remark, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Manufacturer{}, ErrNotFound
	}
	if err != nil {
		return model.Manufacturer{}, err
	}
	return m, nil
}

func (s *SQLiteStore) CreateManufacturer(ctx context.Context, m model.Manufacturer) (model.Manufacturer, error) {
	name, err := validateDimension(m.CompanyID, m.Name)
	if err != nil {
		return model.Manufacturer{}, err
	}
	m.Name = name
	taken, err := s.dimensionNameTaken(ctx, "manufacturers", m.CompanyID, 0, m.Name)
	if err != nil {
		return model.Manufacturer{}, fmt.Errorf("check manufacturer name: %w", err)
	}
	if taken {
		return model.Manufacturer{}, ErrAlreadyExists
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO manufacturers (company_id, name, remark, created_at, updated_at) VALUES (?,?,?,?,?)`,
		m.CompanyID, m.Name, m.Remark, now, now)
	if err != nil {
		return model.Manufacturer{}, fmt.Errorf("insert manufacturer: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.Manufacturer{}, fmt.Errorf("manufacturer id: %w", err)
	}
	m.ID, m.CreatedAt, m.UpdatedAt = id, now, now
	return m, nil
}

func (s *SQLiteStore) ListManufacturers(ctx context.Context, f DimensionListFilter) ([]model.Manufacturer, int64, error) {
	page, size := normalizeDispatchPage(f.Page, f.PageSize)
	where, args := dimensionWhere(f.CompanyID, f.Keyword)
	var total int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM manufacturers WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count manufacturers: %w", err)
	}
	args = append(args, size, (page-1)*size)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+manufacturerColumns+` FROM manufacturers WHERE `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list manufacturers: %w", err)
	}
	defer rows.Close()
	items := make([]model.Manufacturer, 0, size)
	for rows.Next() {
		m, err := scanManufacturerRow(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, m)
	}
	return items, total, rows.Err()
}

func (s *SQLiteStore) UpdateManufacturer(ctx context.Context, m model.Manufacturer) (model.Manufacturer, error) {
	name, err := validateDimension(m.CompanyID, m.Name)
	if err != nil {
		return model.Manufacturer{}, err
	}
	if m.ID == 0 {
		return model.Manufacturer{}, fmt.Errorf("id required")
	}
	m.Name = name
	taken, err := s.dimensionNameTaken(ctx, "manufacturers", m.CompanyID, m.ID, m.Name)
	if err != nil {
		return model.Manufacturer{}, fmt.Errorf("check manufacturer name: %w", err)
	}
	if taken {
		return model.Manufacturer{}, ErrAlreadyExists
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE manufacturers SET name = ?, remark = ?, updated_at = ?
		 WHERE id = ? AND company_id = ? AND deleted_at IS NULL`,
		m.Name, m.Remark, time.Now().UTC(), m.ID, m.CompanyID)
	if err != nil {
		return model.Manufacturer{}, fmt.Errorf("update manufacturer: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return model.Manufacturer{}, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return model.Manufacturer{}, ErrNotFound
	}
	return scanManufacturerRow(s.db.QueryRowContext(ctx,
		`SELECT `+manufacturerColumns+` FROM manufacturers WHERE id = ? AND company_id = ? AND deleted_at IS NULL`,
		m.ID, m.CompanyID))
}

func (s *SQLiteStore) DeleteManufacturer(ctx context.Context, companyID, id int64) error {
	return s.deleteDimension(ctx, "manufacturers", "manufacturer", companyID, id)
}

// ---- 供应商 ----

const supplierColumns = `id, company_id, name, contact_name, phone, remark, created_at, updated_at`

func scanSupplierRow(row interface{ Scan(...any) error }) (model.Supplier, error) {
	var sup model.Supplier
	err := row.Scan(&sup.ID, &sup.CompanyID, &sup.Name, &sup.ContactName, &sup.Phone, &sup.Remark, &sup.CreatedAt, &sup.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Supplier{}, ErrNotFound
	}
	if err != nil {
		return model.Supplier{}, err
	}
	return sup, nil
}

func (s *SQLiteStore) CreateSupplier(ctx context.Context, sup model.Supplier) (model.Supplier, error) {
	name, err := validateDimension(sup.CompanyID, sup.Name)
	if err != nil {
		return model.Supplier{}, err
	}
	sup.Name = name
	taken, err := s.dimensionNameTaken(ctx, "suppliers", sup.CompanyID, 0, sup.Name)
	if err != nil {
		return model.Supplier{}, fmt.Errorf("check supplier name: %w", err)
	}
	if taken {
		return model.Supplier{}, ErrAlreadyExists
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO suppliers (company_id, name, contact_name, phone, remark, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?)`,
		sup.CompanyID, sup.Name, sup.ContactName, sup.Phone, sup.Remark, now, now)
	if err != nil {
		return model.Supplier{}, fmt.Errorf("insert supplier: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.Supplier{}, fmt.Errorf("supplier id: %w", err)
	}
	sup.ID, sup.CreatedAt, sup.UpdatedAt = id, now, now
	return sup, nil
}

func (s *SQLiteStore) ListSuppliers(ctx context.Context, f DimensionListFilter) ([]model.Supplier, int64, error) {
	page, size := normalizeDispatchPage(f.Page, f.PageSize)
	where, args := dimensionWhere(f.CompanyID, f.Keyword)
	var total int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM suppliers WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count suppliers: %w", err)
	}
	args = append(args, size, (page-1)*size)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+supplierColumns+` FROM suppliers WHERE `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list suppliers: %w", err)
	}
	defer rows.Close()
	items := make([]model.Supplier, 0, size)
	for rows.Next() {
		sup, err := scanSupplierRow(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, sup)
	}
	return items, total, rows.Err()
}

func (s *SQLiteStore) UpdateSupplier(ctx context.Context, sup model.Supplier) (model.Supplier, error) {
	name, err := validateDimension(sup.CompanyID, sup.Name)
	if err != nil {
		return model.Supplier{}, err
	}
	if sup.ID == 0 {
		return model.Supplier{}, fmt.Errorf("id required")
	}
	sup.Name = name
	taken, err := s.dimensionNameTaken(ctx, "suppliers", sup.CompanyID, sup.ID, sup.Name)
	if err != nil {
		return model.Supplier{}, fmt.Errorf("check supplier name: %w", err)
	}
	if taken {
		return model.Supplier{}, ErrAlreadyExists
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE suppliers SET name = ?, contact_name = ?, phone = ?, remark = ?, updated_at = ?
		 WHERE id = ? AND company_id = ? AND deleted_at IS NULL`,
		sup.Name, sup.ContactName, sup.Phone, sup.Remark, time.Now().UTC(), sup.ID, sup.CompanyID)
	if err != nil {
		return model.Supplier{}, fmt.Errorf("update supplier: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return model.Supplier{}, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return model.Supplier{}, ErrNotFound
	}
	return scanSupplierRow(s.db.QueryRowContext(ctx,
		`SELECT `+supplierColumns+` FROM suppliers WHERE id = ? AND company_id = ? AND deleted_at IS NULL`,
		sup.ID, sup.CompanyID))
}

func (s *SQLiteStore) DeleteSupplier(ctx context.Context, companyID, id int64) error {
	return s.deleteDimension(ctx, "suppliers", "supplier", companyID, id)
}

// ---- 位置库 ----

const locationColumns = `id, company_id, name, parent_id, remark, created_at, updated_at`

func scanLocationRow(row interface{ Scan(...any) error }) (model.Location, error) {
	var (
		l        model.Location
		parentID sql.NullInt64
	)
	err := row.Scan(&l.ID, &l.CompanyID, &l.Name, &parentID, &l.Remark, &l.CreatedAt, &l.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Location{}, ErrNotFound
	}
	if err != nil {
		return model.Location{}, err
	}
	if parentID.Valid {
		pid := parentID.Int64
		l.ParentID = &pid
	}
	return l, nil
}

func (s *SQLiteStore) CreateLocation(ctx context.Context, l model.Location) (model.Location, error) {
	name, err := validateDimension(l.CompanyID, l.Name)
	if err != nil {
		return model.Location{}, err
	}
	l.Name = name
	taken, err := s.dimensionNameTaken(ctx, "locations", l.CompanyID, 0, l.Name)
	if err != nil {
		return model.Location{}, fmt.Errorf("check location name: %w", err)
	}
	if taken {
		return model.Location{}, ErrAlreadyExists
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO locations (company_id, name, parent_id, remark, created_at, updated_at)
		 VALUES (?,?,?,?,?,?)`,
		l.CompanyID, l.Name, nullableInt64(l.ParentID), l.Remark, now, now)
	if err != nil {
		return model.Location{}, fmt.Errorf("insert location: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.Location{}, fmt.Errorf("location id: %w", err)
	}
	l.ID, l.CreatedAt, l.UpdatedAt = id, now, now
	return l, nil
}

func (s *SQLiteStore) ListLocations(ctx context.Context, f DimensionListFilter) ([]model.Location, int64, error) {
	page, size := normalizeDispatchPage(f.Page, f.PageSize)
	where, args := dimensionWhere(f.CompanyID, f.Keyword)
	var total int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM locations WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count locations: %w", err)
	}
	args = append(args, size, (page-1)*size)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+locationColumns+` FROM locations WHERE `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list locations: %w", err)
	}
	defer rows.Close()
	items := make([]model.Location, 0, size)
	for rows.Next() {
		l, err := scanLocationRow(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, l)
	}
	return items, total, rows.Err()
}

func (s *SQLiteStore) UpdateLocation(ctx context.Context, l model.Location) (model.Location, error) {
	name, err := validateDimension(l.CompanyID, l.Name)
	if err != nil {
		return model.Location{}, err
	}
	if l.ID == 0 {
		return model.Location{}, fmt.Errorf("id required")
	}
	l.Name = name
	taken, err := s.dimensionNameTaken(ctx, "locations", l.CompanyID, l.ID, l.Name)
	if err != nil {
		return model.Location{}, fmt.Errorf("check location name: %w", err)
	}
	if taken {
		return model.Location{}, ErrAlreadyExists
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE locations SET name = ?, parent_id = ?, remark = ?, updated_at = ?
		 WHERE id = ? AND company_id = ? AND deleted_at IS NULL`,
		l.Name, nullableInt64(l.ParentID), l.Remark, time.Now().UTC(), l.ID, l.CompanyID)
	if err != nil {
		return model.Location{}, fmt.Errorf("update location: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return model.Location{}, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return model.Location{}, ErrNotFound
	}
	return scanLocationRow(s.db.QueryRowContext(ctx,
		`SELECT `+locationColumns+` FROM locations WHERE id = ? AND company_id = ? AND deleted_at IS NULL`,
		l.ID, l.CompanyID))
}

func (s *SQLiteStore) DeleteLocation(ctx context.Context, companyID, id int64) error {
	return s.deleteDimension(ctx, "locations", "location", companyID, id)
}

// ---- 型号库 ----

const assetModelColumns = `id, company_id, name, category_id, manufacturer_id, depreciation_id, eol_months, remark, created_at, updated_at`

func scanAssetModelRow(row interface{ Scan(...any) error }) (model.AssetModel, error) {
	var (
		m            model.AssetModel
		manufacturer sql.NullInt64
		depreciation sql.NullInt64
	)
	err := row.Scan(&m.ID, &m.CompanyID, &m.Name, &m.CategoryID, &manufacturer, &depreciation,
		&m.EOLMonths, &m.Remark, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.AssetModel{}, ErrNotFound
	}
	if err != nil {
		return model.AssetModel{}, err
	}
	if manufacturer.Valid {
		id := manufacturer.Int64
		m.ManufacturerID = &id
	}
	if depreciation.Valid {
		id := depreciation.Int64
		m.DepreciationID = &id
	}
	return m, nil
}

func (s *SQLiteStore) CreateAssetModel(ctx context.Context, m model.AssetModel) (model.AssetModel, error) {
	name, err := validateDimension(m.CompanyID, m.Name)
	if err != nil {
		return model.AssetModel{}, err
	}
	m.Name = name
	taken, err := s.dimensionNameTaken(ctx, "asset_models", m.CompanyID, 0, m.Name)
	if err != nil {
		return model.AssetModel{}, fmt.Errorf("check asset model name: %w", err)
	}
	if taken {
		return model.AssetModel{}, ErrAlreadyExists
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO asset_models
		 (company_id, name, category_id, manufacturer_id, depreciation_id, eol_months, remark, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		m.CompanyID, m.Name, m.CategoryID, nullableInt64(m.ManufacturerID),
		nullableInt64(m.DepreciationID), m.EOLMonths, m.Remark, now, now)
	if err != nil {
		return model.AssetModel{}, fmt.Errorf("insert asset model: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.AssetModel{}, fmt.Errorf("asset model id: %w", err)
	}
	m.ID, m.CreatedAt, m.UpdatedAt = id, now, now
	return m, nil
}

func (s *SQLiteStore) ListAssetModels(ctx context.Context, f AssetModelListFilter) ([]model.AssetModel, int64, error) {
	page, size := normalizeDispatchPage(f.Page, f.PageSize)
	where, args := dimensionWhere(f.CompanyID, f.Keyword)
	if f.CategoryID > 0 {
		where += ` AND category_id = ?`
		args = append(args, f.CategoryID)
	}
	var total int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM asset_models WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count asset models: %w", err)
	}
	args = append(args, size, (page-1)*size)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+assetModelColumns+` FROM asset_models WHERE `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list asset models: %w", err)
	}
	defer rows.Close()
	items := make([]model.AssetModel, 0, size)
	for rows.Next() {
		m, err := scanAssetModelRow(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, m)
	}
	return items, total, rows.Err()
}

func (s *SQLiteStore) UpdateAssetModel(ctx context.Context, m model.AssetModel) (model.AssetModel, error) {
	name, err := validateDimension(m.CompanyID, m.Name)
	if err != nil {
		return model.AssetModel{}, err
	}
	if m.ID == 0 {
		return model.AssetModel{}, fmt.Errorf("id required")
	}
	m.Name = name
	taken, err := s.dimensionNameTaken(ctx, "asset_models", m.CompanyID, m.ID, m.Name)
	if err != nil {
		return model.AssetModel{}, fmt.Errorf("check asset model name: %w", err)
	}
	if taken {
		return model.AssetModel{}, ErrAlreadyExists
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE asset_models SET name = ?, category_id = ?, manufacturer_id = ?, depreciation_id = ?,
		 eol_months = ?, remark = ?, updated_at = ?
		 WHERE id = ? AND company_id = ? AND deleted_at IS NULL`,
		m.Name, m.CategoryID, nullableInt64(m.ManufacturerID), nullableInt64(m.DepreciationID),
		m.EOLMonths, m.Remark, time.Now().UTC(), m.ID, m.CompanyID)
	if err != nil {
		return model.AssetModel{}, fmt.Errorf("update asset model: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return model.AssetModel{}, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return model.AssetModel{}, ErrNotFound
	}
	return scanAssetModelRow(s.db.QueryRowContext(ctx,
		`SELECT `+assetModelColumns+` FROM asset_models WHERE id = ? AND company_id = ? AND deleted_at IS NULL`,
		m.ID, m.CompanyID))
}

func (s *SQLiteStore) DeleteAssetModel(ctx context.Context, companyID, id int64) error {
	return s.deleteDimension(ctx, "asset_models", "asset model", companyID, id)
}

// ---- 操作日志（P2 体验运营）----

func (s *SQLiteStore) CreateOperationLog(ctx context.Context, log model.OperationLog) error {
	if err := validateOperationLog(&log); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO operation_logs
		 (company_id, user_id, username, role, action, resource, resource_id, path, detail, ip, user_agent, status, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		log.CompanyID, log.UserID, log.Username, log.Role, log.Action, log.Resource,
		log.ResourceID, log.Path, log.Detail, log.IP, log.UserAgent, log.Status, log.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert operation log: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListOperationLogs(ctx context.Context, f OperationLogListFilter) ([]model.OperationLog, int64, error) {
	page, size := normalizeOperationLogPage(f.Page, f.PageSize)

	where := "1=1"
	args := []any{}
	if f.CompanyID > 0 {
		where += " AND company_id = ?"
		args = append(args, f.CompanyID)
	}
	if f.UserID > 0 {
		where += " AND user_id = ?"
		args = append(args, f.UserID)
	}
	if f.Action != "" {
		where += " AND action = ?"
		args = append(args, f.Action)
	}
	if f.Resource != "" {
		where += " AND resource = ?"
		args = append(args, f.Resource)
	}
	if f.ResourceID != "" {
		where += " AND resource_id = ?"
		args = append(args, f.ResourceID)
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		where += " AND username LIKE ?"
		args = append(args, "%"+kw+"%")
	}
	if f.StartTime != nil {
		start := f.StartTime.UTC()
		where += " AND created_at >= ?"
		args = append(args, start)
	}
	if f.EndTime != nil {
		end := f.EndTime.UTC()
		where += " AND created_at <= ?"
		args = append(args, end)
	}

	var total int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM operation_logs WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count operation logs: %w", err)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, company_id, user_id, username, role, action, resource, resource_id,
		        path, detail, ip, user_agent, status, created_at
		 FROM operation_logs WHERE `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list operation logs: %w", err)
	}
	defer rows.Close()

	items := make([]model.OperationLog, 0, size)
	for rows.Next() {
		var l model.OperationLog
		if err := rows.Scan(&l.ID, &l.CompanyID, &l.UserID, &l.Username, &l.Role,
			&l.Action, &l.Resource, &l.ResourceID, &l.Path, &l.Detail, &l.IP,
			&l.UserAgent, &l.Status, &l.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan operation log: %w", err)
		}
		items = append(items, l)
	}
	return items, total, rows.Err()
}

// ---- 消息中心起步：站内信收件箱（P2 体验运营）----

func (s *SQLiteStore) CreateNotification(ctx context.Context, n model.Notification) (model.Notification, error) {
	if err := validateNotification(&n); err != nil {
		return model.Notification{}, err
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO notifications
		 (company_id, user_id, type, title, content, resource, resource_id, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		n.CompanyID, n.UserID, n.Type, n.Title, n.Content, n.Resource, n.ResourceID, now, now)
	if err != nil {
		return model.Notification{}, fmt.Errorf("insert notification: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.Notification{}, fmt.Errorf("notification id: %w", err)
	}
	n.ID, n.CreatedAt, n.UpdatedAt = id, now, now
	return n, nil
}

// notificationWhere 收件箱口径：user_id 是唯一安全边界（UserID ≤ 0 空集）；
// CompanyID 0 = 不过滤公司（顶栏铃铛无公司上下文）
func notificationWhere(f NotificationListFilter) (string, []any) {
	if f.UserID <= 0 {
		return "1=0", nil
	}
	where := "deleted_at IS NULL AND user_id = ?"
	args := []any{f.UserID}
	if f.CompanyID > 0 {
		where += " AND company_id = ?"
		args = append(args, f.CompanyID)
	}
	if f.Unread != nil {
		if *f.Unread {
			where += " AND read_at IS NULL"
		} else {
			where += " AND read_at IS NOT NULL"
		}
	}
	if f.Type != "" {
		where += " AND type = ?"
		args = append(args, f.Type)
	}
	return where, args
}

func (s *SQLiteStore) ListNotifications(ctx context.Context, f NotificationListFilter) ([]model.Notification, int64, error) {
	page, size := normalizeOperationLogPage(f.Page, f.PageSize)
	where, args := notificationWhere(f)

	var total int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notifications WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count notifications: %w", err)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, company_id, user_id, type, title, content, resource, resource_id, read_at, created_at
		 FROM notifications WHERE `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list notifications: %w", err)
	}
	defer rows.Close()

	items := make([]model.Notification, 0, size)
	for rows.Next() {
		var (
			n       model.Notification
			readAt  sql.NullTime
		)
		if err := rows.Scan(&n.ID, &n.CompanyID, &n.UserID, &n.Type, &n.Title,
			&n.Content, &n.Resource, &n.ResourceID, &readAt, &n.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan notification: %w", err)
		}
		if readAt.Valid {
			t := readAt.Time
			n.ReadAt = &t
		}
		items = append(items, n)
	}
	return items, total, rows.Err()
}

func (s *SQLiteStore) CountUnreadNotifications(ctx context.Context, companyID, userID int64) (int64, error) {
	if userID <= 0 {
		return 0, nil
	}
	where, args := "deleted_at IS NULL AND user_id = ? AND read_at IS NULL", []any{userID}
	if companyID > 0 {
		where += " AND company_id = ?"
		args = append(args, companyID)
	}
	var n int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notifications WHERE `+where, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count unread notifications: %w", err)
	}
	return n, nil
}

func (s *SQLiteStore) MarkNotificationRead(ctx context.Context, companyID, userID, id int64, readAt time.Time) error {
	if userID <= 0 {
		return ErrNotFound
	}
	where, args := "id = ? AND user_id = ? AND read_at IS NULL AND deleted_at IS NULL", []any{id, userID}
	if companyID > 0 {
		where += " AND company_id = ?"
		args = append(args, companyID)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET read_at = ?, updated_at = ? WHERE `+where,
		append([]any{readAt, readAt}, args...)...)
	if err != nil {
		return fmt.Errorf("mark notification read: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected > 0 {
		return nil
	}
	checkWhere, checkArgs := "id = ? AND user_id = ? AND deleted_at IS NULL", []any{id, userID}
	if companyID > 0 {
		checkWhere += " AND company_id = ?"
		checkArgs = append(checkArgs, companyID)
	}
	var exists int
	if err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM notifications WHERE `+checkWhere, checkArgs...).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("check notification: %w", err)
	}
	return nil // 已读重复标记：幂等放行
}

func (s *SQLiteStore) MarkAllNotificationsRead(ctx context.Context, companyID, userID int64, readAt time.Time) (int64, error) {
	if userID <= 0 {
		return 0, nil
	}
	where, args := "user_id = ? AND read_at IS NULL AND deleted_at IS NULL", []any{userID}
	if companyID > 0 {
		where += " AND company_id = ?"
		args = append(args, companyID)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET read_at = ?, updated_at = ? WHERE `+where,
		append([]any{readAt, readAt}, args...)...)
	if err != nil {
		return 0, fmt.Errorf("mark all notifications read: %w", err)
	}
	return res.RowsAffected()
}

// ---- 软件许可（P2 体验运营）----

const licenseColumns = `id, company_id, name, vendor, category, license_key, total_seats,
	purchase_date, expiration_date, termination_date, remark, created_at, updated_at`

func scanLicenseRow(row interface{ Scan(...any) error }) (model.License, error) {
	var (
		l            model.License
		purchaseAt   sql.NullTime
		expirationAt sql.NullTime
		terminateAt  sql.NullTime
	)
	err := row.Scan(&l.ID, &l.CompanyID, &l.Name, &l.Vendor, &l.Category, &l.LicenseKey,
		&l.TotalSeats, &purchaseAt, &expirationAt, &terminateAt, &l.Remark, &l.CreatedAt, &l.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.License{}, ErrNotFound
	}
	if err != nil {
		return model.License{}, err
	}
	if purchaseAt.Valid {
		l.PurchaseDate = &purchaseAt.Time
	}
	if expirationAt.Valid {
		l.ExpirationDate = &expirationAt.Time
	}
	if terminateAt.Valid {
		l.TerminationDate = &terminateAt.Time
	}
	return l, nil
}

func (s *SQLiteStore) CreateLicense(ctx context.Context, l model.License) (model.License, error) {
	l, err := validateLicense(l)
	if err != nil {
		return model.License{}, err
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO licenses
		 (company_id, name, vendor, category, license_key, total_seats,
		  purchase_date, expiration_date, termination_date, remark, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		l.CompanyID, l.Name, l.Vendor, l.Category, l.LicenseKey, l.TotalSeats,
		l.PurchaseDate, l.ExpirationDate, l.TerminationDate, l.Remark, now, now)
	if err != nil {
		return model.License{}, fmt.Errorf("insert license: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.License{}, fmt.Errorf("license id: %w", err)
	}
	l.ID, l.CreatedAt, l.UpdatedAt = id, now, now
	return l, nil
}

func (s *SQLiteStore) ListLicenses(ctx context.Context, f LicenseListFilter) ([]model.License, int64, error) {
	page, size := normalizeDispatchPage(f.Page, f.PageSize)
	where := "company_id = ? AND deleted_at IS NULL"
	args := []any{f.CompanyID}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		where += " AND (name LIKE ? OR vendor LIKE ?)"
		args = append(args, "%"+kw+"%", "%"+kw+"%")
	}
	if f.ExpiringDays > 0 {
		now := time.Now().UTC()
		where += " AND termination_date IS NULL AND expiration_date IS NOT NULL AND expiration_date > ? AND expiration_date <= ?"
		args = append(args, now, now.AddDate(0, 0, f.ExpiringDays))
	}
	var total int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM licenses WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count licenses: %w", err)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+licenseColumns+` FROM licenses WHERE `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list licenses: %w", err)
	}
	defer rows.Close()
	items := make([]model.License, 0, size)
	for rows.Next() {
		l, err := scanLicenseRow(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, l)
	}
	return items, total, rows.Err()
}

func (s *SQLiteStore) GetLicense(ctx context.Context, companyID, id int64) (model.License, error) {
	return scanLicenseRow(s.db.QueryRowContext(ctx,
		`SELECT `+licenseColumns+` FROM licenses WHERE id = ? AND company_id = ? AND deleted_at IS NULL`,
		id, companyID))
}

func (s *SQLiteStore) UpdateLicense(ctx context.Context, l model.License) (model.License, error) {
	l, err := validateLicense(l)
	if err != nil {
		return model.License{}, err
	}
	if l.ID == 0 {
		return model.License{}, fmt.Errorf("id required")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE licenses SET name = ?, vendor = ?, category = ?, license_key = ?, total_seats = ?,
			purchase_date = ?, expiration_date = ?, termination_date = ?, remark = ?, updated_at = ?
		 WHERE id = ? AND company_id = ? AND deleted_at IS NULL`,
		l.Name, l.Vendor, l.Category, l.LicenseKey, l.TotalSeats,
		l.PurchaseDate, l.ExpirationDate, l.TerminationDate, l.Remark, time.Now().UTC(),
		l.ID, l.CompanyID)
	if err != nil {
		return model.License{}, fmt.Errorf("update license: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return model.License{}, ErrNotFound
	}
	return s.GetLicense(ctx, l.CompanyID, l.ID)
}

func (s *SQLiteStore) DeleteLicense(ctx context.Context, companyID, id int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE licenses SET deleted_at = ? WHERE id = ? AND company_id = ? AND deleted_at IS NULL`,
		time.Now().UTC(), id, companyID)
	if err != nil {
		return fmt.Errorf("delete license: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- 耗材管理（P2 体验运营）----

const consumableColumns = `id, company_id, name, spec, unit, stock, min_quantity, remark, created_at, updated_at`

func scanConsumableRow(row interface{ Scan(...any) error }) (model.Consumable, error) {
	var c model.Consumable
	err := row.Scan(&c.ID, &c.CompanyID, &c.Name, &c.Spec, &c.Unit, &c.Stock,
		&c.MinQuantity, &c.Remark, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Consumable{}, ErrNotFound
	}
	if err != nil {
		return model.Consumable{}, err
	}
	return c, nil
}

func (s *SQLiteStore) CreateConsumable(ctx context.Context, c model.Consumable) (model.Consumable, error) {
	c, err := validateConsumable(c)
	if err != nil {
		return model.Consumable{}, err
	}
	taken, err := s.dimensionNameTaken(ctx, "consumables", c.CompanyID, 0, c.Name)
	if err != nil {
		return model.Consumable{}, fmt.Errorf("check consumable name: %w", err)
	}
	if taken {
		return model.Consumable{}, ErrAlreadyExists
	}
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO consumables (company_id, name, spec, unit, stock, min_quantity, remark, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		c.CompanyID, c.Name, c.Spec, c.Unit, 0, c.MinQuantity, c.Remark, now, now)
	if err != nil {
		return model.Consumable{}, fmt.Errorf("insert consumable: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.Consumable{}, fmt.Errorf("consumable id: %w", err)
	}
	c.ID, c.Stock, c.CreatedAt, c.UpdatedAt = id, 0, now, now
	return c, nil
}

func (s *SQLiteStore) ListConsumables(ctx context.Context, f ConsumableListFilter) ([]model.Consumable, int64, error) {
	page, size := normalizeDispatchPage(f.Page, f.PageSize)
	where := "company_id = ? AND deleted_at IS NULL"
	args := []any{f.CompanyID}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		where += " AND (name LIKE ? OR spec LIKE ?)"
		args = append(args, "%"+kw+"%", "%"+kw+"%")
	}
	if f.LowStock != nil {
		if *f.LowStock {
			where += " AND min_quantity > 0 AND stock <= min_quantity"
		} else {
			where += " AND NOT (min_quantity > 0 AND stock <= min_quantity)"
		}
	}
	var total int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM consumables WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count consumables: %w", err)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+consumableColumns+` FROM consumables WHERE `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list consumables: %w", err)
	}
	defer rows.Close()
	items := make([]model.Consumable, 0, size)
	for rows.Next() {
		c, err := scanConsumableRow(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, c)
	}
	return items, total, rows.Err()
}

func (s *SQLiteStore) UpdateConsumable(ctx context.Context, c model.Consumable) (model.Consumable, error) {
	c, err := validateConsumable(c)
	if err != nil {
		return model.Consumable{}, err
	}
	if c.ID == 0 {
		return model.Consumable{}, fmt.Errorf("id required")
	}
	taken, err := s.dimensionNameTaken(ctx, "consumables", c.CompanyID, c.ID, c.Name)
	if err != nil {
		return model.Consumable{}, fmt.Errorf("check consumable name: %w", err)
	}
	if taken {
		return model.Consumable{}, ErrAlreadyExists
	}
	// 库存不经编辑面：UPDATE 语句不含 stock 列
	res, err := s.db.ExecContext(ctx,
		`UPDATE consumables SET name = ?, spec = ?, unit = ?, min_quantity = ?, remark = ?, updated_at = ?
		 WHERE id = ? AND company_id = ? AND deleted_at IS NULL`,
		c.Name, c.Spec, c.Unit, c.MinQuantity, c.Remark, time.Now().UTC(), c.ID, c.CompanyID)
	if err != nil {
		return model.Consumable{}, fmt.Errorf("update consumable: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return model.Consumable{}, ErrNotFound
	}
	return scanConsumableRow(s.db.QueryRowContext(ctx,
		`SELECT `+consumableColumns+` FROM consumables WHERE id = ? AND company_id = ? AND deleted_at IS NULL`,
		c.ID, c.CompanyID))
}

func (s *SQLiteStore) DeleteConsumable(ctx context.Context, companyID, id int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE consumables SET deleted_at = ? WHERE id = ? AND company_id = ? AND deleted_at IS NULL`,
		time.Now().UTC(), id, companyID)
	if err != nil {
		return fmt.Errorf("delete consumable: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	return nil
}

// CreateConsumableTxn 流水 + 库存原子变更：显式事务内条件更新
//（stock + delta >= 0）零命中时区分 NotFound 与 ErrInsufficient，
// 并发超卖由条件更新天然拦截，流水不落半截
func (s *SQLiteStore) CreateConsumableTxn(ctx context.Context, txn model.ConsumableTxn) (model.ConsumableTxn, model.Consumable, error) {
	if err := validateConsumableTxn(txn); err != nil {
		return model.ConsumableTxn{}, model.Consumable{}, err
	}
	now := time.Now().UTC()
	txn.CreatedAt = now

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.ConsumableTxn{}, model.Consumable{}, fmt.Errorf("begin txn: %w", err)
	}
	rollback := func() { _ = tx.Rollback() }

	res, err := tx.ExecContext(ctx,
		`UPDATE consumables SET stock = stock + ?, updated_at = ?
		 WHERE id = ? AND company_id = ? AND deleted_at IS NULL AND stock + ? >= 0`,
		txn.Delta, now, txn.ConsumableID, txn.CompanyID, txn.Delta)
	if err != nil {
		rollback()
		return model.ConsumableTxn{}, model.Consumable{}, fmt.Errorf("apply stock: %w", err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		var exists int
		err := tx.QueryRowContext(ctx,
			`SELECT 1 FROM consumables WHERE id = ? AND company_id = ? AND deleted_at IS NULL`,
			txn.ConsumableID, txn.CompanyID).Scan(&exists)
		rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return model.ConsumableTxn{}, model.Consumable{}, ErrNotFound
		}
		if err != nil {
			return model.ConsumableTxn{}, model.Consumable{}, fmt.Errorf("check consumable: %w", err)
		}
		return model.ConsumableTxn{}, model.Consumable{}, ErrInsufficient
	}

	ins, err := tx.ExecContext(ctx,
		`INSERT INTO consumable_txns
		 (company_id, consumable_id, type, delta, recipient, operator_id, operator_name, remark, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		txn.CompanyID, txn.ConsumableID, txn.Type, txn.Delta, txn.Recipient,
		txn.OperatorID, txn.OperatorName, txn.Remark, now)
	if err != nil {
		rollback()
		return model.ConsumableTxn{}, model.Consumable{}, fmt.Errorf("insert txn: %w", err)
	}
	id, err := ins.LastInsertId()
	if err != nil {
		rollback()
		return model.ConsumableTxn{}, model.Consumable{}, fmt.Errorf("txn id: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return model.ConsumableTxn{}, model.Consumable{}, fmt.Errorf("commit txn: %w", err)
	}

	txn.ID = id
	out, err := scanConsumableRow(s.db.QueryRowContext(ctx,
		`SELECT `+consumableColumns+` FROM consumables WHERE id = ? AND company_id = ? AND deleted_at IS NULL`,
		txn.ConsumableID, txn.CompanyID))
	if err != nil {
		return model.ConsumableTxn{}, model.Consumable{}, err
	}
	return txn, out, nil
}

func (s *SQLiteStore) ListConsumableTxns(ctx context.Context, f ConsumableTxnListFilter) ([]model.ConsumableTxn, int64, error) {
	page, size := normalizeDispatchPage(f.Page, f.PageSize)
	where := "company_id = ?"
	args := []any{f.CompanyID}
	if f.ConsumableID > 0 {
		where += " AND consumable_id = ?"
		args = append(args, f.ConsumableID)
	}
	if f.Type != "" {
		where += " AND type = ?"
		args = append(args, f.Type)
	}
	var total int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM consumable_txns WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count consumable txns: %w", err)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, company_id, consumable_id, type, delta, recipient, operator_id, operator_name, remark, created_at
		 FROM consumable_txns WHERE `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`,
		append(args, size, (page-1)*size)...)
	if err != nil {
		return nil, 0, fmt.Errorf("list consumable txns: %w", err)
	}
	defer rows.Close()
	items := make([]model.ConsumableTxn, 0, size)
	for rows.Next() {
		var txn model.ConsumableTxn
		if err := rows.Scan(&txn.ID, &txn.CompanyID, &txn.ConsumableID, &txn.Type, &txn.Delta,
			&txn.Recipient, &txn.OperatorID, &txn.OperatorName, &txn.Remark, &txn.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan consumable txn: %w", err)
		}
		items = append(items, txn)
	}
	return items, total, rows.Err()
}
