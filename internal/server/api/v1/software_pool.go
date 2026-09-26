package v1

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/softwareaudit"
	"itagent/internal/server/store"

	"github.com/gin-gonic/gin"
)

// 阶段三软件合规比对：受控软件池 CRUD（读面登录可读、写面 admin）
// + 合规报表端点（admin-only，报表中心先例）。比对口径单源在
// softwareaudit.BuildCompliance，本层只做装配与落库

type SoftwarePoolHandler struct{}

func RegisterSoftwarePoolRoutes(protected *gin.RouterGroup) {
	h := &SoftwarePoolHandler{}
	read := protected.Group("/software-pools")
	{
		read.GET("", h.List)
	}
	adminOnly := protected.Group("/software-pools")
	adminOnly.Use(middleware.RoleMiddleware("admin"))
	{
		adminOnly.POST("", h.Create)
		adminOnly.PUT("/:id", h.Update)
		adminOnly.DELETE("/:id", h.Delete)
	}
}

type softwarePoolListQuery struct {
	CompanyID int64  `form:"company_id" binding:"required"`
	Keyword   string `form:"keyword"`
	Page      int    `form:"page,default=1"`
	PageSize  int    `form:"page_size,default=50"`
}

func (h *SoftwarePoolHandler) List(c *gin.Context) {
	var q softwarePoolListQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		Fail(c, http.StatusBadRequest, 40001, "company_id 必填")
		return
	}
	db := store.DB.Model(&model.SoftwarePool{}).Where("company_id = ?", q.CompanyID)
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		db = db.Where("name LIKE ?", "%"+kw+"%")
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询受控软件池失败")
		return
	}
	var items []model.SoftwarePool
	if err := db.Order("id desc").
		Offset((q.Page - 1) * q.PageSize).Limit(q.PageSize).Find(&items).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询受控软件池失败")
		return
	}
	enrichSoftwarePools(c, items)
	Success(c, PageResult{Total: total, Items: items})
}

// enrichSoftwarePools 批量富化挂接许可名（防 N+1，license 富化先例）
func enrichSoftwarePools(c *gin.Context, items []model.SoftwarePool) {
	if len(items) == 0 {
		return
	}
	ids := make([]int64, 0, len(items))
	for _, p := range items {
		if p.LicenseID != nil {
			ids = append(ids, *p.LicenseID)
		}
	}
	if len(ids) == 0 {
		return
	}
	var licenses []model.License
	if err := store.DB.WithContext(c.Request.Context()).Select("id, name").Find(&licenses, ids).Error; err != nil {
		return // 富化失败不阻塞读面，回查许可详情兜底
	}
	nameByID := make(map[int64]string, len(licenses))
	for _, l := range licenses {
		nameByID[l.ID] = l.Name
	}
	for i := range items {
		if items[i].LicenseID != nil {
			items[i].LicenseName = nameByID[*items[i].LicenseID]
		}
	}
}

type softwarePoolRequest struct {
	CompanyID int64  `json:"company_id" binding:"required"`
	Name      string `json:"name"`
	Vendor    string `json:"vendor"`
	Category  string `json:"category"`
	LicenseID *int64 `json:"license_id"` // 0/null 解除挂接，>0 校验存在+同公司
	Remark    string `json:"remark"`
}

// validatePoolLicenseRef 挂接许可校验：存在 + 同公司（跨公司按 404），
// 返回归一后的挂接值（0 → nil）。**池项不做席位余量校验**——超用正是
// 合规引擎要发现的问题，而不是建池时要拦的问题
func validatePoolLicenseRef(c *gin.Context, companyID int64, licenseID *int64) (*int64, bool) {
	if licenseID == nil || *licenseID <= 0 {
		return nil, true
	}
	var lic model.License
	if err := store.DB.First(&lic, *licenseID).Error; err != nil || lic.CompanyID != companyID {
		Fail(c, http.StatusNotFound, 40402, "挂接的软件许可不存在")
		return nil, false
	}
	return licenseID, true
}

// poolNameTaken 名称占用检查（软删除记录不占名，维表同口径）
func poolNameTaken(c *gin.Context, companyID, excludeID int64, name string) bool {
	q := store.DB.Model(&model.SoftwarePool{}).
		Where("company_id = ? AND name = ?", companyID, name)
	if excludeID > 0 {
		q = q.Where("id <> ?", excludeID)
	}
	var cnt int64
	if err := q.Count(&cnt).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "校验受控软件名失败")
		return false // 校验失败按占用处理，拒绝写入
	}
	return cnt > 0
}

func (h *SoftwarePoolHandler) Create(c *gin.Context) {
	var req softwarePoolRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		Fail(c, http.StatusBadRequest, 40001, "受控软件名称必填")
		return
	}
	if poolNameTaken(c, req.CompanyID, 0, name) {
		Fail(c, http.StatusConflict, 40901, "该公司已存在同名受控软件")
		return
	}
	licenseID, ok := validatePoolLicenseRef(c, req.CompanyID, req.LicenseID)
	if !ok {
		return
	}
	item := model.SoftwarePool{
		CompanyID: req.CompanyID, Name: name, Vendor: req.Vendor,
		Category: req.Category, LicenseID: licenseID, Remark: req.Remark,
	}
	if err := store.DB.Create(&item).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50002, "创建受控软件失败")
		return
	}
	// 切片副本陷阱：富化改的是切片元素，必须回取切片元素再响应
	enriched := []model.SoftwarePool{item}
	enrichSoftwarePools(c, enriched)
	Success(c, enriched[0])
}

func (h *SoftwarePoolHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid id")
		return
	}
	var req softwarePoolRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	var item model.SoftwarePool
	if err := store.DB.First(&item, id).Error; err != nil || item.CompanyID != req.CompanyID {
		Fail(c, http.StatusNotFound, 40401, "受控软件不存在")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		Fail(c, http.StatusBadRequest, 40001, "受控软件名称必填")
		return
	}
	if poolNameTaken(c, req.CompanyID, item.ID, name) {
		Fail(c, http.StatusConflict, 40901, "该公司已存在同名受控软件")
		return
	}
	licenseID, ok := validatePoolLicenseRef(c, req.CompanyID, req.LicenseID)
	if !ok {
		return
	}
	updates := map[string]interface{}{
		"name":       name,
		"vendor":     req.Vendor,
		"category":   req.Category,
		"license_id": licenseID,
		"remark":     req.Remark,
		"updated_at": time.Now().UTC(),
	}
	if err := store.DB.Model(&item).Updates(updates).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50003, "更新受控软件失败")
		return
	}
	if err := store.DB.First(&item, id).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "重载受控软件失败")
		return
	}
	// 切片副本陷阱：富化必须回取切片元素再响应（license.go 同款教训）
	enriched := []model.SoftwarePool{item}
	enrichSoftwarePools(c, enriched)
	Success(c, enriched[0])
}

func (h *SoftwarePoolHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid id")
		return
	}
	companyID, err := strconv.ParseInt(c.Query("company_id"), 10, 64)
	if err != nil || companyID <= 0 {
		Fail(c, http.StatusBadRequest, 40001, "company_id 必填")
		return
	}
	res := store.DB.Where("id = ? AND company_id = ?", id, companyID).Delete(&model.SoftwarePool{})
	if res.Error != nil {
		Fail(c, http.StatusInternalServerError, 50003, "删除受控软件失败")
		return
	}
	if res.RowsAffected == 0 {
		Fail(c, http.StatusNotFound, 40401, "受控软件不存在")
		return
	}
	Success(c, gin.H{"deleted": true})
}

// ==================== 合规报表 ====================

// RegisterSoftwareComplianceRoutes 合规报表端点：整组 admin-only
//（报表中心先例——盗版嫌疑与超用清单是管理视角数据，不下发普通用户）
func RegisterSoftwareComplianceRoutes(protected *gin.RouterGroup) {
	h := &SoftwarePoolHandler{}
	adminOnly := protected.Group("/software-compliance")
	adminOnly.Use(middleware.RoleMiddleware("admin"))
	{
		adminOnly.GET("", h.Compliance)
	}
}

type softwareComplianceQuery struct {
	CompanyID int64 `form:"company_id" binding:"required"`
	Page      int   `form:"page,default=1"`
	PageSize  int   `form:"page_size,default=50"`
}

// Compliance 合规报表：受控池项全量（含超用标记）+ 未受控商业软件分页
//（安装数降序）+ 汇总卡。比对纯函数单源 softwareaudit.BuildCompliance
func (h *SoftwarePoolHandler) Compliance(c *gin.Context) {
	var q softwareComplianceQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		Fail(c, http.StatusBadRequest, 40001, "company_id 必填")
		return
	}
	ctx := c.Request.Context()

	var pools []model.SoftwarePool
	if err := store.DB.WithContext(ctx).Where("company_id = ?", q.CompanyID).Find(&pools).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询受控软件池失败")
		return
	}
	licenseMap := make(map[int64]model.License, len(pools))
	for _, p := range pools {
		if p.LicenseID != nil {
			if _, ok := licenseMap[*p.LicenseID]; !ok {
				var lic model.License
				if err := store.DB.WithContext(ctx).First(&lic, *p.LicenseID).Error; err == nil {
					licenseMap[lic.ID] = lic
				}
			}
		}
	}
	installs, err := softwareaudit.ListSoftwareInstalls(ctx, store.DB, q.CompanyID)
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "聚合终端软件安装失败")
		return
	}

	report := softwareaudit.BuildCompliance(pools, licenseMap, installs)

	// 未受控清单分页（服务端已按安装数降序）
	total := len(report.Unmanaged)
	start := (q.Page - 1) * q.PageSize
	if start > total {
		start = total
	}
	end := start + q.PageSize
	if end > total {
		end = total
	}
	pageItems := report.Unmanaged[start:end]

	Success(c, gin.H{
		"summary":    report.Summary,
		"pool_items": report.PoolItems,
		"unmanaged":  gin.H{"total": total, "items": pageItems},
	})
}
