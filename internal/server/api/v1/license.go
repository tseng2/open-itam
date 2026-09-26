package v1

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// P2 软件许可管理：商业授权池管理面。读面开放所有登录用户
//（席位查看/资产表单下拉依赖），写面仅 admin。状态与已用席位不落库：
// 状态由日期经 model.ResolveLicenseStatus 派生，席位由资产挂接计数富化。
// 席位挂接/解绑走台账建账/编辑（assets.license_id），超用拦截在 asset.go

type LicenseHandler struct {
	store *store.GormStore
}

// RegisterLicenseRoutes 注册许可路由：同前缀读/写两组注册（dimension 先例）
func RegisterLicenseRoutes(protected *gin.RouterGroup) {
	h := &LicenseHandler{store: store.NewGormStore(store.DB)}

	read := protected.Group("/licenses")
	{
		read.GET("", h.List)
		read.GET("/:id", h.Get)
	}
	adminOnly := protected.Group("/licenses")
	adminOnly.Use(middleware.RoleMiddleware("admin"))
	{
		adminOnly.POST("", h.Create)
		adminOnly.PUT("/:id", h.Update)
		adminOnly.DELETE("/:id", h.Delete)
	}
}

type licenseListQuery struct {
	CompanyID    int64  `form:"company_id" binding:"required"`
	Keyword      string `form:"keyword"`
	ExpiringDays int    `form:"expiring_days"` // >0 = 只看 N 天内到期且未终止（到期提醒）
	Page         int    `form:"page,default=1"`
	PageSize     int    `form:"page_size,default=50"`
}

func (h *LicenseHandler) List(c *gin.Context) {
	var q licenseListQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		Fail(c, http.StatusBadRequest, 40001, "company_id 必填")
		return
	}
	items, total, err := h.store.ListLicenses(c.Request.Context(), store.LicenseListFilter{
		CompanyID: q.CompanyID, Keyword: q.Keyword, ExpiringDays: q.ExpiringDays,
		Page: q.Page, PageSize: q.PageSize,
	})
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询软件许可失败")
		return
	}
	enrichLicenses(c.Request.Context(), items)
	Success(c, PageResult{Total: total, Items: items})
}

func (h *LicenseHandler) Get(c *gin.Context) {
	id, ok := dimensionIDParam(c)
	if !ok {
		return
	}
	companyID, ok := dimensionCompanyQuery(c)
	if !ok {
		return
	}
	l, err := h.store.GetLicense(c.Request.Context(), companyID, id)
	if err != nil {
		failLicenseStoreError(c, err, "查询软件许可失败")
		return
	}
	enriched := []model.License{l}
	enrichLicenses(c.Request.Context(), enriched)
	Success(c, enriched[0])
}
// enrichLicenses 派生字段富化：状态由日期派生（口径在 model 层单源），
// 已用席位按资产挂接一次 GROUP BY 计数；失败安静降级为 0（合规面以
// 列表为准，富化不阻塞读面）
func enrichLicenses(ctx context.Context, items []model.License) {
	if len(items) == 0 {
		return
	}
	now := time.Now().UTC()
	ids := make([]int64, 0, len(items))
	for i := range items {
		items[i].Status = model.ResolveLicenseStatus(now, items[i].ExpirationDate, items[i].TerminationDate)
		ids = append(ids, items[i].ID)
	}
	type seatRow struct {
		LicenseID int64
		Used      int64
	}
	var rows []seatRow
	if err := store.DB.WithContext(ctx).Model(&model.Asset{}).
		Select("license_id, COUNT(*) AS used").
		Where("license_id IN ?", ids).
		Group("license_id").Scan(&rows).Error; err != nil {
		return
	}
	used := make(map[int64]int64, len(rows))
	for _, row := range rows {
		used[row.LicenseID] = row.Used
	}
	for i := range items {
		items[i].UsedSeats = used[items[i].ID]
	}
}

type licenseRequest struct {
	CompanyID       int64      `json:"company_id" binding:"required"`
	Name            string     `json:"name"`
	Vendor          string     `json:"vendor"`
	Category        string     `json:"category"`
	LicenseKey      string     `json:"license_key"`
	TotalSeats      int        `json:"total_seats"`
	PurchaseDate    *time.Time `json:"purchase_date"`
	ExpirationDate  *time.Time `json:"expiration_date"`
	TerminationDate *time.Time `json:"termination_date"`
	Remark          string     `json:"remark"`
}

func (h *LicenseHandler) Create(c *gin.Context) {
	var req licenseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	created, err := h.store.CreateLicense(c.Request.Context(), model.License{
		CompanyID: req.CompanyID, Name: req.Name, Vendor: req.Vendor, Category: req.Category,
		LicenseKey: req.LicenseKey, TotalSeats: req.TotalSeats,
		PurchaseDate: req.PurchaseDate, ExpirationDate: req.ExpirationDate,
		TerminationDate: req.TerminationDate, Remark: req.Remark,
	})
	if err != nil {
		failLicenseStoreError(c, err, "创建软件许可失败")
		return
	}
	// 切片元素是值拷贝：富化后必须回取切片内容再响应（P1 已知陷阱）
	enriched := []model.License{created}
	enrichLicenses(c.Request.Context(), enriched)
	Success(c, enriched[0])
}

func (h *LicenseHandler) Update(c *gin.Context) {
	id, ok := dimensionIDParam(c)
	if !ok {
		return
	}
	var req licenseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	updated, err := h.store.UpdateLicense(c.Request.Context(), model.License{
		BaseModel:       model.BaseModel{ID: id},
		CompanyID:       req.CompanyID, Name: req.Name, Vendor: req.Vendor, Category: req.Category,
		LicenseKey:      req.LicenseKey, TotalSeats: req.TotalSeats,
		PurchaseDate:    req.PurchaseDate, ExpirationDate: req.ExpirationDate,
		TerminationDate: req.TerminationDate, Remark: req.Remark,
	})
	if err != nil {
		failLicenseStoreError(c, err, "更新软件许可失败")
		return
	}
	enriched := []model.License{updated}
	enrichLicenses(c.Request.Context(), enriched)
	Success(c, enriched[0])
}

// Delete 删除拦截：席位仍被资产挂接的许可不允许删（解绑历史会丢），
// 409 带引用计数（dimension 先例——SQLiteStore 测试库无 assets 表，
// 计数只在生产 GormStore 的 gin 面执行）
func (h *LicenseHandler) Delete(c *gin.Context) {
	id, ok := dimensionIDParam(c)
	if !ok {
		return
	}
	companyID, ok := dimensionCompanyQuery(c)
	if !ok {
		return
	}
	assetRefs, ok := countRefs(c, &model.Asset{}, "company_id = ? AND license_id = ?", companyID, id)
	if !ok {
		return
	}
	if assetRefs > 0 {
		failDimensionInUse(c, assetRefs, "软件许可")
		return
	}
	// 阶段三：受控软件池也挂接许可（席位来源），挂接中同样拦截删除
	poolRefs, ok := countRefs(c, &model.SoftwarePool{}, "company_id = ? AND license_id = ?", companyID, id)
	if !ok {
		return
	}
	if poolRefs > 0 {
		failDimensionInUse(c, poolRefs, "软件许可")
		return
	}
	if err := h.store.DeleteLicense(c.Request.Context(), companyID, id); err != nil {
		failLicenseStoreError(c, err, "删除软件许可失败")
		return
	}
	Success(c, gin.H{"deleted": true})
}

func failLicenseStoreError(c *gin.Context, err error, action string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		Fail(c, http.StatusNotFound, 40401, "软件许可不存在")
	default:
		Fail(c, http.StatusInternalServerError, 50001, action)
	}
}

// ==================== 台账席位挂接（assets.license_id 联动）====================

// countLicenseSeats 统计许可当前已占席位；excludeAssetID > 0 时排除该资产
//（编辑场景：资产换绑同一许可不得自占一席）
func countLicenseSeats(ctx context.Context, companyID, licenseID, excludeAssetID int64) (int64, error) {
	q := store.DB.WithContext(ctx).Model(&model.Asset{}).
		Where("company_id = ? AND license_id = ?", companyID, licenseID)
	if excludeAssetID > 0 {
		q = q.Where("id <> ?", excludeAssetID)
	}
	var used int64
	if err := q.Count(&used).Error; err != nil {
		return 0, err
	}
	return used, nil
}

// validateAssetLicenseRef 台账挂接许可校验：存在 + 同公司（404），
// 席位超用拦截（409）——total_seats=0 不限席位；被压缩席位数造成的
// 历史超用不做追溯修正，由许可页的合规度视图呈现
func validateAssetLicenseRef(c *gin.Context, companyID, licenseID, excludeAssetID int64) (*model.License, bool) {
	var l model.License
	if err := store.DB.Where("id = ? AND company_id = ?", licenseID, companyID).First(&l).Error; err != nil {
		Fail(c, http.StatusNotFound, 40402, "软件许可不存在或不属于该公司")
		return nil, false
	}
	if l.TotalSeats > 0 {
		used, err := countLicenseSeats(c.Request.Context(), companyID, licenseID, excludeAssetID)
		if err != nil {
			Fail(c, http.StatusInternalServerError, 50001, "查询许可席位失败")
			return nil, false
		}
		if used >= int64(l.TotalSeats) {
			Fail(c, http.StatusConflict, 40903,
				"软件许可席位已满（"+strconv.Itoa(l.TotalSeats)+" 席），请先解绑其他资产")
			return nil, false
		}
	}
	return &l, true
}
