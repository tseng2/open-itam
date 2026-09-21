package model

import "time"

// Device 代表由 Agent 动态上报维护的终端设备特征及网络/系统画像
type Device struct {
	BaseModel
	AssetID       *int64    `gorm:"uniqueIndex" json:"asset_id"`                            // 关联绑定的实物资产ID (1:1)
	Asset         *Asset    `gorm:"foreignKey:AssetID" json:"asset,omitempty"`
	DeviceID      string    `gorm:"type:varchar(64);uniqueIndex;not null" json:"device_id"` // Agent根据硬件主板/网卡生成的防伪机器指纹
	Hostname      string    `gorm:"type:varchar(128);index" json:"hostname"`                // 当前主机名
	OSName        string    `gorm:"type:varchar(128)" json:"os_name"`                       // 操作系统名称与版本
	Architecture  string    `gorm:"type:varchar(32)" json:"architecture"`                   // 系统架构 (amd64 / arm64)
	IPAddress     string    `gorm:"type:varchar(64)" json:"ip_address"`                     // 当前主要内网IP
	PublicIP      string    `gorm:"type:varchar(64)" json:"public_ip"`                      // 当前公网IP
	MacAddress    string    `gorm:"type:varchar(64)" json:"mac_address"`                    // 主网卡MAC
	CPUModel      string    `gorm:"type:varchar(128)" json:"cpu_model"`                     // CPU型号
	MemoryTotalGB float64   `gorm:"type:decimal(8,2)" json:"memory_total_gb"`               // 内存容量(GB)
	DiskTotalGB   float64   `gorm:"type:decimal(10,2)" json:"disk_total_gb"`                // 磁盘总容量(GB)
	// 身份特征包：新指纹注册时与失联老终端做归属调和（重装系统丢配置的场景）
	BIOSUUID      string    `gorm:"type:varchar(64);index" json:"bios_uuid"`                // BIOS/整机 UUID
	BoardSerial   string    `gorm:"type:varchar(128)" json:"board_serial"`                  // 主板序列号
	MACSet        string    `gorm:"type:text" json:"mac_set"`                               // 全部物理网卡 MAC（JSON 数组，已排序）
	AgentVersion  string    `gorm:"type:varchar(32)" json:"agent_version"`                  // Agent采集端版本
	LastSeenAt    time.Time `gorm:"index" json:"last_seen_at"`                              // 最近心跳时间
}

func (Device) TableName() string {
	return "agent_devices"
}
