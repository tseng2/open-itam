package v1

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/geoip"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// Agent 采集与失联判定配置面（agent_settings 单例）：
// 读写均 admin（Settings 页管理面）；Agent 侧经老栈 agent/config 与
// ingest 响应接收下发，不直接访问本端点。
//
// EffectiveAgentSettings 是全部消费点的统一入口（阈值判定/频率下发/
// 漫游地理基准）——设置页保存立即生效（每次判定实时读库），进程内
// 不做缓存，避免「改了配置要重启」的隐性契约

// EffectiveAgentSettings 读取生效配置：单例行优先，无行 / 查询失败 /
// store 未初始化（纯单测环境）回落内置默认。全字段防御归一由保存校验
// 保证，读出的值即可信值
func EffectiveAgentSettings() model.AgentSettings {
	if store.DB == nil {
		return model.DefaultAgentSettings()
	}
	st := store.NewGormStore(store.DB)
	cfg, err := st.GetAgentSettings(context.Background())
	if err != nil {
		return model.DefaultAgentSettings()
	}
	return cfg
}

// EffectivePresenceTimeout 生效失联阈值（秒 → 时长归一，口径单源）
func EffectivePresenceTimeout() time.Duration {
	return model.ResolveOfflineThreshold(EffectiveAgentSettings().OfflineThresholdSec)
}

// EffectivePresenceGeo 漫游判定的地理维度注入（公司省 + 内嵌 GeoIP 库）。
// 库不可用时 RegionOf 恒返回空——presence 判定自动跳过地理维度
func EffectivePresenceGeo() model.PresenceGeo {
	return model.PresenceGeo{
		HomeProvince: EffectiveAgentSettings().CompanyProvince,
		RegionOf:     geoip.RegionOf,
	}
}

type AgentSettingsHandler struct{}

func RegisterAgentSettingsRoutes(protected *gin.RouterGroup) {
	h := &AgentSettingsHandler{}
	g := protected.Group("/agent-settings")
	g.GET("", middleware.RoleMiddleware("admin"), h.Get)
	g.PUT("", middleware.RoleMiddleware("admin"), h.Update)
}

// Get 读生效配置：未保存过时返回内置默认（首次打开设置页的展示值）
func (h *AgentSettingsHandler) Get(c *gin.Context) {
	Success(c, EffectiveAgentSettings())
}

type UpdateAgentSettingsRequest struct {
	HeartbeatIntervalSec int    `json:"heartbeat_interval_sec" binding:"required"`
	FullIntervalSec      int    `json:"full_interval_sec" binding:"required"`
	OfflineThresholdSec  int    `json:"offline_threshold_sec" binding:"required"`
	CompanyProvince      string `json:"company_province"`
}

// Update 保存配置（全字段必填 PUT）。联动红线校验：
// 频率与阈值均须为正；full ≥ heartbeat（全量是心跳的超集动作）；
// 阈值必须大于心跳周期——否则健康终端的心跳间隔本身就击穿阈值，
// 全员假失联。公司省份可空（空 = 跳过地理维判定），非空须与
// ip2region 库名口径一致（如「广东省」）
func (h *AgentSettingsHandler) Update(c *gin.Context) {
	var req UpdateAgentSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	if req.HeartbeatIntervalSec <= 0 || req.FullIntervalSec <= 0 || req.OfflineThresholdSec <= 0 {
		Fail(c, http.StatusBadRequest, 40002, "采集频率与失联阈值必须为正整数（秒）")
		return
	}
	if req.FullIntervalSec < req.HeartbeatIntervalSec {
		Fail(c, http.StatusBadRequest, 40002, "全量上报周期不能小于心跳周期")
		return
	}
	if req.OfflineThresholdSec <= req.HeartbeatIntervalSec {
		Fail(c, http.StatusBadRequest, 40002, "失联阈值必须大于心跳周期，否则健康终端会被误判失联")
		return
	}

	cfg := model.AgentSettings{
		ID:                  model.AgentSettingsSingletonID,
		HeartbeatIntervalSec: req.HeartbeatIntervalSec,
		FullIntervalSec:      req.FullIntervalSec,
		OfflineThresholdSec:  req.OfflineThresholdSec,
		CompanyProvince:      req.CompanyProvince,
	}
	if err := store.NewGormStore(store.DB).PutAgentSettings(c.Request.Context(), cfg); err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "保存采集配置失败")
		return
	}
	Success(c, EffectiveAgentSettings())
}
