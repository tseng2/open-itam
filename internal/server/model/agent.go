package model

import (
	"encoding/json"
	"time"
)

type AgentDevice struct {
	DeviceID     string    `gorm:"type:varchar(64);primaryKey" json:"device_id"`
	DeviceToken  string    `gorm:"type:varchar(64);not null" json:"-"`
	Hostname     string    `gorm:"type:varchar(128);not null;default:''" json:"hostname"`
	OS           string    `gorm:"type:varchar(128);not null;default:''" json:"os"`
	AgentVersion string    `gorm:"type:varchar(32);not null;default:''" json:"agent_version"`
	LastSeenAt   time.Time `gorm:"not null" json:"last_seen_at"`
	RegisteredAt time.Time `gorm:"not null" json:"registered_at"`
}

func (AgentDevice) TableName() string {
	return "devices"
}

type AgentReport struct {
	ID         int64           `gorm:"primaryKey;autoIncrement" json:"id"`
	DeviceID   string          `gorm:"type:varchar(64);index:idx_reports_device,priority:1;not null" json:"device_id"`
	ReportType string          `gorm:"type:varchar(32);not null" json:"report_type"`
	Payload    json.RawMessage `gorm:"type:json;not null" json:"payload"`
	ReportedAt time.Time       `gorm:"not null" json:"reported_at"`
	ReceivedAt time.Time       `gorm:"not null" json:"received_at"`
}

func (AgentReport) TableName() string {
	return "reports"
}

type AgentSnapshot struct {
	DeviceID  string          `gorm:"type:varchar(64);primaryKey" json:"device_id"`
	Payload   json.RawMessage `gorm:"type:json;not null" json:"payload"`
	UpdatedAt time.Time       `gorm:"not null" json:"updated_at"`
}

func (AgentSnapshot) TableName() string {
	return "snapshots"
}

type AgentChangeEvent struct {
	ID        int64             `gorm:"primaryKey;autoIncrement" json:"id"`
	DeviceID  string            `gorm:"type:varchar(64);not null" json:"device_id"`
	Kind      string            `gorm:"type:varchar(32);not null" json:"kind"`
	Severity  string            `gorm:"type:varchar(32);not null" json:"severity"`
	Message   string            `gorm:"type:text;not null" json:"message"`
	Detail    json.RawMessage   `gorm:"type:json" json:"detail,omitempty"`
	Acked     bool              `gorm:"index:idx_change_events_open,priority:1;not null;default:false" json:"acked"`
	CreatedAt time.Time         `gorm:"not null" json:"created_at"`
}

func (AgentChangeEvent) TableName() string {
	return "change_events"
}
