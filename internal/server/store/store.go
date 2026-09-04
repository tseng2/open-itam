package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrNotFound      = errors.New("record not found")
	ErrUnauthorized  = errors.New("invalid credentials")
	ErrAlreadyExists = errors.New("record already exists")
)

type Device struct {
	DeviceID     string    `json:"device_id"`
	DeviceToken  string    `json:"-"`
	Hostname     string    `json:"hostname"`
	OS           string    `json:"os"`
	AgentVersion string    `json:"agent_version"`
	LastSeenAt   time.Time `json:"last_seen_at"`
	RegisteredAt time.Time `json:"registered_at"`
}

type Report struct {
	ID           int64     `json:"id"`
	DeviceID     string    `json:"device_id"`
	ReportType   string    `json:"report_type"`
	Payload      json.RawMessage `json:"payload"`
	ReportedAt   time.Time `json:"reported_at"`
	ReceivedAt   time.Time `json:"received_at"`
}

type Store interface {
	RegisterDevice(ctx context.Context, d Device) (token string, err error)
	Authenticate(ctx context.Context, deviceID, token string) error
	UpsertDeviceSeen(ctx context.Context, d Device) error
	SaveReport(ctx context.Context, r Report) (int64, error)
	GetDevice(ctx context.Context, deviceID string) (Device, error)
	ListDevices(ctx context.Context, limit, offset int) ([]Device, error)
	ListReports(ctx context.Context, deviceID string, limit, offset int) ([]Report, error)
	Close() error
}
