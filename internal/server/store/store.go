package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"itagent/internal/server/model"
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
	ID         int64           `json:"id"`
	DeviceID   string          `json:"device_id"`
	ReportType string          `json:"report_type"`
	Payload    json.RawMessage `json:"payload"`
	ReportedAt time.Time       `json:"reported_at"`
	ReceivedAt time.Time       `json:"received_at"`
}

type Snapshot struct {
	DeviceID  string
	Payload   json.RawMessage
	UpdatedAt time.Time
}

type ChangeEvent struct {
	ID        int64             `json:"id"`
	DeviceID  string            `json:"device_id"`
	Kind      string            `json:"kind"`
	Severity  string            `json:"severity"`
	Message   string            `json:"message"`
	Detail    map[string]string `json:"detail,omitempty"`
	Acked     bool              `json:"acked"`
	CreatedAt time.Time         `json:"created_at"`
}

type Store interface {
	RegisterDevice(ctx context.Context, d Device) (token string, err error)
	Authenticate(ctx context.Context, deviceID, token string) error
	UpsertDeviceSeen(ctx context.Context, d Device) error
	SaveReport(ctx context.Context, r Report) (int64, error)
	GetDevice(ctx context.Context, deviceID string) (Device, error)
	ListDevices(ctx context.Context, limit, offset int) ([]Device, error)
	ListReports(ctx context.Context, deviceID string, limit, offset int) ([]Report, error)
	GetSnapshot(ctx context.Context, deviceID string) (Snapshot, error)
	SaveSnapshot(ctx context.Context, s Snapshot) error
	SaveChangeEvent(ctx context.Context, e ChangeEvent) (int64, error)
	ListChangeEvents(ctx context.Context, includeAcked bool, limit, offset int) ([]ChangeEvent, error)
	AckChangeEvent(ctx context.Context, id int64) error
	// 防护模块（防退出/防卸载）与卸载验证码：密码归服务端集中管理
	GetProtectionModule(ctx context.Context, key string) (model.ProtectionModule, error)
	PutProtectionModule(ctx context.Context, m model.ProtectionModule) error
	CreateUninstallCode(ctx context.Context, deviceID string, ttl time.Duration) (model.UninstallCode, error)
	VerifyUninstallCode(ctx context.Context, deviceID, code string) error
	Close() error
}
