package v1

import (
	"net/http"
	"time"

	"itagent/internal/server/model"
	"itagent/internal/server/store"
	"github.com/gin-gonic/gin"
)

type AgentHandler struct{}

func RegisterAgentRoutes(r *gin.RouterGroup) {
	h := &AgentHandler{}
	agent := r.Group("/agent")
	{
		agent.POST("/heartbeat", h.Heartbeat)
	}
}

type HeartbeatPayload struct {
	DeviceID      string  `json:"device_id" binding:"required"` // 终端指纹
	Hostname      string  `json:"hostname"`
	OSName        string  `json:"os_name"`
	Architecture  string  `json:"architecture"`
	IPAddress     string  `json:"ip_address"`
	MacAddress    string  `json:"mac_address"`
	CPUModel      string  `json:"cpu_model"`
	MemoryTotalGB float64 `json:"memory_total_gb"`
	DiskTotalGB   float64 `json:"disk_total_gb"`
	AgentVersion  string  `json:"agent_version"`
}

// Heartbeat 接收 Agent 定时动态上报，自动绑定或更新终端硬件指纹状态
func (h *AgentHandler) Heartbeat(c *gin.Context) {
	var req HeartbeatPayload
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}

	var device model.Device
	err := store.DB.Where("device_id = ?", req.DeviceID).First(&device).Error
	now := time.Now()

	if err != nil {
		// 新上报的终端指纹，自动建立 Device 记录
		device = model.Device{
			DeviceID:      req.DeviceID,
			Hostname:      req.Hostname,
			OSName:        req.OSName,
			Architecture:  req.Architecture,
			IPAddress:     req.IPAddress,
			MacAddress:    req.MacAddress,
			CPUModel:      req.CPUModel,
			MemoryTotalGB: req.MemoryTotalGB,
			DiskTotalGB:   req.DiskTotalGB,
			AgentVersion:  req.AgentVersion,
			LastSeenAt:    now,
		}
		if err := store.DB.Create(&device).Error; err != nil {
			Fail(c, http.StatusInternalServerError, 50001, "failed to register agent device")
			return
		}
	} else {
		// 已存在终端，更新动态指标与心跳时间
		device.Hostname = req.Hostname
		device.OSName = req.OSName
		device.Architecture = req.Architecture
		device.IPAddress = req.IPAddress
		device.MacAddress = req.MacAddress
		device.CPUModel = req.CPUModel
		device.MemoryTotalGB = req.MemoryTotalGB
		device.DiskTotalGB = req.DiskTotalGB
		device.AgentVersion = req.AgentVersion
		device.LastSeenAt = now
		store.DB.Save(&device)
	}

	Success(c, gin.H{
		"device_id":    device.DeviceID,
		"last_seen_at": device.LastSeenAt,
	})
}
