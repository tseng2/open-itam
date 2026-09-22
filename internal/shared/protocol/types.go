package protocol

import (
	"encoding/json"
	"time"
)

const (
	ReportTypeHeartbeat = "heartbeat"
	ReportTypeFull      = "full"
)

type Envelope struct {
	DeviceID     string          `json:"device_id"`
	AgentVersion string          `json:"agent_version"`
	ReportType   string          `json:"report_type"`
	ReportedAt   time.Time       `json:"reported_at"`
	Payload      json.RawMessage `json:"payload"`
}

func (e *Envelope) Validate() error {
	if e.DeviceID == "" {
		return &ValidationError{Field: "device_id", Msg: "required"}
	}
	if e.ReportType != ReportTypeHeartbeat && e.ReportType != ReportTypeFull {
		return &ValidationError{Field: "report_type", Msg: "must be heartbeat or full"}
	}
	if e.ReportedAt.IsZero() {
		return &ValidationError{Field: "reported_at", Msg: "required"}
	}
	return nil
}

type ValidationError struct {
	Field string
	Msg   string
}

func (e *ValidationError) Error() string {
	return "invalid field " + e.Field + ": " + e.Msg
}

type HeartbeatPayload struct {
	Hostname  string    `json:"hostname"`
	OS        OSInfo    `json:"os"`
	Logon     LogonInfo `json:"logon"`
	BootTime  time.Time `json:"boot_time"`
	UptimeSec int64     `json:"uptime_sec"`
	Network   Network   `json:"network"`
}

type LogonInfo struct {
	User    string    `json:"logon_user"`
	Domain  string    `json:"logon_domain"`
	Type    string    `json:"logon_type"`
	LogonAt time.Time `json:"logon_at"`
}

const (
	LogonTypeAD    = "ad"
	LogonTypeLocal = "local"
)

type OSInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Build   string `json:"build"`
}

type Network struct {
	Interfaces []NetInterface `json:"interfaces"`
	PublicIP   string         `json:"public_ip"`
}

type NetInterface struct {
	Name string   `json:"name"`
	MAC  string   `json:"mac"`
	IPs  []string `json:"ips"`
	IsUp bool     `json:"is_up"`
}

type FullPayload struct {
	HeartbeatPayload
	Hardware Hardware   `json:"hardware"`
	Software []Software `json:"software"`
}

type Hardware struct {
	Brand         string         `json:"brand"`
	Model         string         `json:"model"`
	Serial        string         `json:"serial"`
	BIOSSerial    string         `json:"bios_serial"`
	UUID          string         `json:"uuid"`        // Win32_ComputerSystemProduct.UUID，组装机身份主锚点
	BoardSerial   string         `json:"board_serial"` // Win32_BaseBoard.SerialNumber
	CPU           []CPU          `json:"cpu"`
	MemoryTotalMB int64          `json:"memory_total_mb"`
	MemoryModules []MemoryModule `json:"memory_modules"`
	Disks         []Disk         `json:"disks"`
	GPUs          []GPU          `json:"gpus"`
	NICs          []NIC          `json:"nics"`
	SMART         []SmartHealth  `json:"disk_smart_health"`
}

type CPU struct {
	Model   string `json:"model"`
	Cores   int    `json:"cores"`
	Threads int    `json:"threads"`
}

type MemoryModule struct {
	Slot     string `json:"slot"`
	SizeMB   int64  `json:"size_mb"`
	Type     string `json:"type"`
	SpeedMHz int    `json:"speed_mhz"`
	Serial   string `json:"serial"`
}

type Disk struct {
	Model     string `json:"model"`
	SizeGB    int64  `json:"size_gb"`
	Type      string `json:"type"`
	Serial    string `json:"serial"`
	Removable bool   `json:"removable"`
}

type GPU struct {
	Model  string `json:"model"`
	VRAMMB int64  `json:"vram_mb"`
}

type NIC struct {
	Name      string   `json:"name"`
	MAC       string   `json:"mac"`
	SpeedMbps int      `json:"speed_mbps"`
	IPs       []string `json:"ips,omitempty"`
}

type SmartHealth struct {
	DiskSerial          string `json:"disk_serial"`
	Model               string `json:"model"`
	OverallHealth       string `json:"overall_health"`
	ReallocatedSectors  int64  `json:"reallocated_sectors"`
	PendingSectors      int64  `json:"pending_sectors"`
	PowerOnHours        int64  `json:"power_on_hours"`
	TemperatureC        int    `json:"temperature_c"`
	PercentLifetimeUsed int    `json:"percent_lifetime_used"`
}

type Software struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	InstallPath string `json:"install_path"`
}

type RegisterRequest struct {
	DeviceID     string   `json:"device_id"`
	Hostname     string   `json:"hostname"`
	OS           string   `json:"os"`
	AgentVersion string   `json:"agent_version"`
	InstallToken string   `json:"install_token"`
	// 身份特征包：服务端用于新指纹与失联老终端的归属调和
	BiosUUID    string   `json:"bios_uuid,omitempty"`
	BoardSerial string   `json:"board_serial,omitempty"`
	MACs        []string `json:"macs,omitempty"`
}

type RegisterResponse struct {
	Code        int    `json:"code"`
	Message     string `json:"message"`
	DeviceToken string `json:"device_token,omitempty"`
	// 非空表示服务端识别出本机是既有终端，要求 Agent 改用该旧指纹保持数据连续
	AdoptDeviceID string `json:"adopt_device_id,omitempty"`
}

// UpdateInfo 服务端在 ingest 响应中下发的 Agent 更新指令
type UpdateInfo struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	Notes   string `json:"notes,omitempty"`
}

type IngestResponse struct {
	Code             int         `json:"code"`
	Message          string      `json:"message"`
	ServerTime       string      `json:"server_time"`
	NextHeartbeatSec int         `json:"next_heartbeat_sec"`
	NextFullSec      int         `json:"next_full_sec"`
	Update           *UpdateInfo `json:"update,omitempty"`
}

// UninstallCodeVerifyRequest Agent 端在线校验卸载验证码的请求体：
// 服务端按 device_token + device_id 绑定校验，通过即标记已用（单次有效）
type UninstallCodeVerifyRequest struct {
	Code string `json:"code"`
}
