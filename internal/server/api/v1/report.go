package v1

import (
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// P2 报表中心：管理视角聚合面（CIYO 汇总 8 卡 / 年度资产价值 /
// 月度建账趋势 / 分布环图）。admin-only；聚合口径只此一处：
// 状态计数含报废（台账全量），价值合计与年度价值为在册口径
//（非报废），月度趋势为建账口径（含报废，反映录入节奏）。
// 日期桶在 Go 侧聚合——DB 侧 DATE_FORMAT 是方言，双库不可移植

// ==================== 聚合纯函数（口径单源）====================

// annualValueSample 年度价值样本：一台在册资产的购入价值对
type annualValueSample struct {
	PurchaseDate time.Time
	Original     float64
	Net          float64
}

type reportAnnualRow struct {
	Year     int     `json:"year"`
	Count    int64   `json:"count"`
	Original float64 `json:"original"`
	Net      float64 `json:"net"`
}

// groupAnnualValue 近 N 年购入资产聚合：窗口年降序、空年零填充、
// 窗口外样本丢弃（更早年份属历史归档口径，不入窗口图）
func groupAnnualValue(now time.Time, years int, samples []annualValueSample) []reportAnnualRow {
	years = clampReportWindow(years, 1, 20)
	thisYear := now.Year()
	rows := make([]reportAnnualRow, 0, years)
	index := make(map[int]int, years)
	for y := thisYear; y > thisYear-years; y-- {
		index[y] = len(rows)
		rows = append(rows, reportAnnualRow{Year: y})
	}
	for _, s := range samples {
		if i, ok := index[s.PurchaseDate.Year()]; ok {
			rows[i].Count++
			rows[i].Original += s.Original
			rows[i].Net += s.Net
		}
	}
	return rows
}

type reportTrendRow struct {
	Month string `json:"month"`
	Count int64  `json:"count"`
}

// bucketMonthlyTrend 近 N 月建账趋势：月键升序、空月零填充、
// 窗口外样本丢弃；月界按自然月（1 号）切分
func bucketMonthlyTrend(now time.Time, months int, created []time.Time) []reportTrendRow {
	months = clampReportWindow(months, 1, 36)
	base := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	keys := make([]string, 0, months)
	index := make(map[string]int, months)
	for i := months - 1; i >= 0; i-- {
		key := base.AddDate(0, -i, 0).Format("2006-01")
		index[key] = len(keys)
		keys = append(keys, key)
	}
	rows := make([]reportTrendRow, len(keys))
	for i, key := range keys {
		rows[i] = reportTrendRow{Month: key}
	}
	for _, t := range created {
		if i, ok := index[t.Format("2006-01")]; ok {
			rows[i].Count++
		}
	}
	return rows
}

type reportDistributionRow struct {
	Label   string `json:"label"`
	Count   int64  `json:"count"`
	Percent int    `json:"percent"`
}

// distributionRows 分布行：计数降序（同数按标签字典序稳定排列），
// 百分比四舍五入到整数；总数为零返回空（除零防御）
func distributionRows(counts map[string]int64) []reportDistributionRow {
	var total int64
	for _, c := range counts {
		total += c
	}
	if total == 0 {
		return []reportDistributionRow{}
	}
	rows := make([]reportDistributionRow, 0, len(counts))
	for label, c := range counts {
		rows = append(rows, reportDistributionRow{
			Label:   label,
			Count:   c,
			Percent: int(math.Round(float64(c) * 100 / float64(total))),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Label < rows[j].Label
	})
	return rows
}

// clampReportWindow 窗口参数兜底（缺省/非法/超大一律收敛到边界内）
func clampReportWindow(v, def, max int) int {
	if v < def {
		return def
	}
	if v > max {
		return max
	}
	return v
}

// reportQueryPositive 正整数 query 参数解析（缺席/非法返回缺省值）
func reportQueryPositive(c *gin.Context, key string, def int) int {
	v, err := strconv.Atoi(c.Query(key))
	if err != nil || v <= 0 {
		return def
	}
	return v
}

// ==================== gin 路由与查询 ====================

type ReportHandler struct {
	store *store.GormStore
}

// RegisterReportRoutes 报表中心全部只读、仅 admin（RBAC 收口在路由组）
func RegisterReportRoutes(protected *gin.RouterGroup) {
	h := &ReportHandler{store: store.NewGormStore(store.DB)}
	adminOnly := protected.Group("/reports")
	adminOnly.Use(middleware.RoleMiddleware("admin"))
	{
		adminOnly.GET("/summary", h.Summary)
		adminOnly.GET("/annual-value", h.AnnualValue)
		adminOnly.GET("/monthly-trend", h.MonthlyTrend)
		adminOnly.GET("/distribution", h.Distribution)
	}
}

// reportSummary 汇总 8 卡载荷（字段名即前后端契约）
type reportSummary struct {
	TotalAssets int64   `json:"total_assets"`
	InUse       int64   `json:"in_use"`
	Stock       int64   `json:"stock"`
	Repair      int64   `json:"repair"`
	Scrapped    int64   `json:"scrapped"`
	OffBook     int64   `json:"off_book"`
	TotalValue  float64 `json:"total_value"`
	NetValue    float64 `json:"net_value"`
}

// Summary 汇总 8 卡：一次拉取公司全量台账的轻量列（状态/列管/价值），
// 在 Go 侧聚合——表量级由导出上限（5000）天然封顶，无需分页聚合
func (h *ReportHandler) Summary(c *gin.Context) {
	companyID, ok := dimensionCompanyQuery(c)
	if !ok {
		return
	}
	var rows []model.Asset
	if err := store.DB.WithContext(c.Request.Context()).Model(&model.Asset{}).
		Where("company_id = ?", companyID).
		Select("status", "off_book", "original_price", "net_value").
		Find(&rows).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询资产汇总失败")
		return
	}
	s := reportSummary{TotalAssets: int64(len(rows))}
	for _, a := range rows {
		switch a.Status {
		case model.AssetStatusInUse:
			s.InUse++
		case model.AssetStatusStock:
			s.Stock++
		case model.AssetStatusRepair:
			s.Repair++
		case model.AssetStatusScrapped:
			s.Scrapped++
		}
		// 列管 = 财务销账且未报废（off_book 与运营状态正交，P0-β 契约）
		if a.OffBook && a.Status != model.AssetStatusScrapped {
			s.OffBook++
		}
		// 价值合计 = 在册口径（报废资产残值出表）
		if a.Status != model.AssetStatusScrapped {
			s.TotalValue += a.OriginalPrice
			s.NetValue += a.NetValue
		}
	}
	Success(c, s)
}

// AnnualValue 年度资产价值：近 N 年（默认 6）购入聚合，在册口径
func (h *ReportHandler) AnnualValue(c *gin.Context) {
	companyID, ok := dimensionCompanyQuery(c)
	if !ok {
		return
	}
	years := reportQueryPositive(c, "years", 6)
	var rows []model.Asset
	if err := store.DB.WithContext(c.Request.Context()).Model(&model.Asset{}).
		Where("company_id = ? AND status <> ? AND purchase_date IS NOT NULL",
			companyID, model.AssetStatusScrapped).
		Select("purchase_date", "original_price", "net_value").
		Find(&rows).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询年度资产价值失败")
		return
	}
	samples := make([]annualValueSample, 0, len(rows))
	for _, a := range rows {
		if a.PurchaseDate != nil {
			samples = append(samples, annualValueSample{
				PurchaseDate: *a.PurchaseDate, Original: a.OriginalPrice, Net: a.NetValue,
			})
		}
	}
	Success(c, gin.H{"rows": groupAnnualValue(time.Now().UTC(), years, samples)})
}

// MonthlyTrend 月度建账趋势：近 N 月（默认 12）新建台账数（建账口径含报废）
func (h *ReportHandler) MonthlyTrend(c *gin.Context) {
	companyID, ok := dimensionCompanyQuery(c)
	if !ok {
		return
	}
	months := reportQueryPositive(c, "months", 12)
	now := time.Now().UTC()
	windowStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).
		AddDate(0, -(clampReportWindow(months, 1, 36) - 1), 0)
	var rows []model.Asset
	if err := store.DB.WithContext(c.Request.Context()).Model(&model.Asset{}).
		Where("company_id = ? AND created_at >= ?", companyID, windowStart).
		Select("created_at").
		Find(&rows).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询月度趋势失败")
		return
	}
	created := make([]time.Time, 0, len(rows))
	for _, a := range rows {
		created = append(created, a.CreatedAt)
	}
	Success(c, gin.H{"rows": bucketMonthlyTrend(now, months, created)})
}

// Distribution 分布环图：dimension=status（默认）| category
func (h *ReportHandler) Distribution(c *gin.Context) {
	companyID, ok := dimensionCompanyQuery(c)
	if !ok {
		return
	}
	dimension := "status"
	if d := c.Query("dimension"); d != "" {
		dimension = d
	}
	if dimension != "status" && dimension != "category" {
		Fail(c, http.StatusBadRequest, 40003, "dimension 仅支持 status / category")
		return
	}
	var rows []model.Asset
	if err := store.DB.WithContext(c.Request.Context()).Model(&model.Asset{}).
		Where("company_id = ?", companyID).
		Select("status", "category_id").
		Find(&rows).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询资产分布失败")
		return
	}
	counts := make(map[string]int64)
	for _, a := range rows {
		label := "未知"
		if dimension == "status" {
			if name := model.AssetStatusName(a.Status); name != "" {
				label = name
			}
		} else if name := model.AssetCategoryName(a.CategoryID); name != "" {
			label = name
		}
		counts[label]++
	}
	Success(c, gin.H{"rows": distributionRows(counts)})
}
