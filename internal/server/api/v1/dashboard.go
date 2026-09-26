package v1

import (
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// 总览大盘轻聚合端点：全员全公司概览页的数据面（登录即可读）。
// 与报表中心 /reports/summary 的口径边界：报表是 admin 单公司管理口径，
// 大盘是跨公司全局口径；两者共用 model.AssetStatusName 段位与
// 台账全量（含报废、软删行不计）的状态计数语义，禁止各自重写。
//
// 失联判定阈值不再启动注入：agent_settings 单例是唯一源，
// 判定时经 EffectivePresenceTimeout 实时读取（公式同 ResolvePresence）：
// now - last_seen > 阈值即失联

// dashboardAgentVersionTopN Agent 版本分布返回条数上限（计数降序截断），
// 防御病态多版本刷爆载荷；大盘 UI 只展示前几行
const dashboardAgentVersionTopN = 5

type DashboardHandler struct{}

// RegisterDashboardRoutes 大盘汇总：只读、全员登录可读（不带 RoleMiddleware）
func RegisterDashboardRoutes(protected *gin.RouterGroup) {
	h := &DashboardHandler{}
	dashboard := protected.Group("/dashboard")
	dashboard.GET("/summary", h.Summary)
}

// dashboardAssetsCard 资产状态计数：状态段位与 model.AssetStatusName 单源
type dashboardAssetsCard struct {
	Total    int64 `json:"total"`
	InUse    int64 `json:"in_use"`
	Stock    int64 `json:"stock"`
	Repair   int64 `json:"repair"`
	Scrapped int64 `json:"scrapped"`
}

// dashboardDevicesCard 终端心跳三数：active = last_seen 在阈值内
type dashboardDevicesCard struct {
	Total   int64 `json:"total"`
	Active  int64 `json:"active"`
	Missing int64 `json:"missing"`
}

type dashboardVersionRow struct {
	Version string `json:"version"`
	Count   int64 `json:"count"`
	Percent int    `json:"percent"`
}

type dashboardSummary struct {
	Assets         dashboardAssetsCard  `json:"assets"`
	ChangesPending int64                `json:"changes_pending"`
	Devices        dashboardDevicesCard  `json:"devices"`
	AgentVersions  []dashboardVersionRow `json:"agent_versions"`
}

// Summary 一次请求聚合大盘全部数字：资产状态计数（GROUP BY）+ changes
// 未 ack 计数 + 终端活跃/失联 + Agent 版本分布。终端量级小（企业终端
// 数百级），直接拉轻量列在 Go 侧聚合——规避跨驱动时间比较方言，与
// report.go 先例同套路
func (h *DashboardHandler) Summary(c *gin.Context) {
	ctx := c.Request.Context()

	var statusRows []struct {
		Status int
		Count  int64
	}
	// GORM 默认作用域排除软删行（同资产列表口径）；状态计数含报废（台账全量）
	if err := store.DB.WithContext(ctx).Model(&model.Asset{}).
		Select("status, COUNT(*) AS count").Group("status").Scan(&statusRows).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询资产状态计数失败")
		return
	}
	s := dashboardSummary{AgentVersions: []dashboardVersionRow{}}
	for _, row := range statusRows {
		s.Assets.Total += row.Count
		switch row.Status {
		case model.AssetStatusInUse:
			s.Assets.InUse = row.Count
		case model.AssetStatusStock:
			s.Assets.Stock = row.Count
		case model.AssetStatusRepair:
			s.Assets.Repair = row.Count
		case model.AssetStatusScrapped:
			s.Assets.Scrapped = row.Count
		}
	}

	// 待处理告警与变更：谓词与 store.ListChangeEvents(includeAcked=false) 单源
	if err := store.DB.WithContext(ctx).Model(&model.AgentChangeEvent{}).
		Where("acked = ?", false).Count(&s.ChangesPending).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询待处理变更失败")
		return
	}

	var devices []model.AgentDevice
	if err := store.DB.WithContext(ctx).Model(&model.AgentDevice{}).
		Select("agent_version", "last_seen_at").Find(&devices).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询终端状态失败")
		return
	}
	now := time.Now()
	versionCounts := make(map[string]int64)
	for _, d := range devices {
		s.Devices.Total++
		// 失联公式同 ResolvePresence：now - last_seen > 阈值
		if now.Sub(d.LastSeenAt) > EffectivePresenceTimeout() {
			s.Devices.Missing++
		} else {
			s.Devices.Active++
		}
		if d.AgentVersion != "" {
			versionCounts[d.AgentVersion]++
		}
	}
	if len(versionCounts) > 0 {
		s.AgentVersions = agentVersionRows(versionCounts, s.Devices.Total)
	}
	Success(c, s)
}

// agentVersionRows 版本分布行：计数降序（同数按版本字典序降序稳定排列），
// 百分比按注册终端总数四舍五入，截断 TopN
func agentVersionRows(counts map[string]int64, total int64) []dashboardVersionRow {
	rows := make([]dashboardVersionRow, 0, len(counts))
	for version, count := range counts {
		rows = append(rows, dashboardVersionRow{Version: version, Count: count})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Version > rows[j].Version
	})
	if len(rows) > dashboardAgentVersionTopN {
		rows = rows[:dashboardAgentVersionTopN]
	}
	for i := range rows {
		rows[i].Percent = int(math.Round(float64(rows[i].Count) * 100 / float64(total)))
	}
	return rows
}
