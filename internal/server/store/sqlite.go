package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
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
