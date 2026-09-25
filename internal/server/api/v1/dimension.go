package v1

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"
)

// DimensionHandler 维度治理管理面（P1）：厂商/供应商/位置库/型号库
// 四张维表的 CRUD。读面开放给所有登录用户（资产表单下拉依赖），
// 写面仅 admin；删除时做引用拦截（资产挂接/型号引用厂商/子位置引用父级）。
// 资产侧挂接联动与富化在 asset.go，口径共口不在此复制
type DimensionHandler struct {
	store *store.GormStore
}

// RegisterDimensionRoutes 注册四组维度路由（同前缀读/写两组注册，
// 方法树不冲突——asset-requests 先例）；命名风格与挂载表前缀一一对应
func RegisterDimensionRoutes(protected *gin.RouterGroup) {
	h := &DimensionHandler{store: store.NewGormStore(store.DB)}

	register := func(prefix string, list, create, update, del gin.HandlerFunc) {
		read := protected.Group(prefix)
		{
			read.GET("", list)
		}
		adminOnly := protected.Group(prefix)
		adminOnly.Use(middleware.RoleMiddleware("admin"))
		{
			adminOnly.POST("", create)
			adminOnly.PUT("/:id", update)
			adminOnly.DELETE("/:id", del)
		}
	}
	register("/manufacturers", h.ListManufacturers, h.CreateManufacturer, h.UpdateManufacturer, h.DeleteManufacturer)
	register("/suppliers", h.ListSuppliers, h.CreateSupplier, h.UpdateSupplier, h.DeleteSupplier)
	register("/locations", h.ListLocations, h.CreateLocation, h.UpdateLocation, h.DeleteLocation)
	register("/asset-models", h.ListAssetModels, h.CreateAssetModel, h.UpdateAssetModel, h.DeleteAssetModel)
}

// dimensionListQuery 维表列表共用查询参数（型号库多一个 category_id）
type dimensionListQuery struct {
	CompanyID int64  `form:"company_id" binding:"required"`
	Keyword   string `form:"keyword"`
	Page      int    `form:"page,default=1"`
	PageSize  int    `form:"page_size,default=50"`
}

type assetModelListQuery struct {
	CompanyID  int64  `form:"company_id" binding:"required"`
	CategoryID int64  `form:"category_id"`
	Keyword    string `form:"keyword"`
	Page       int    `form:"page,default=1"`
	PageSize   int    `form:"page_size,default=50"`
}

func dimensionIDParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid id")
		return 0, false
	}
	return id, true
}

func dimensionCompanyQuery(c *gin.Context) (int64, bool) {
	companyID, err := strconv.ParseInt(c.Query("company_id"), 10, 64)
	if err != nil || companyID <= 0 {
		Fail(c, http.StatusBadRequest, 40001, "company_id 必填")
		return 0, false
	}
	return companyID, true
}

// failDimensionStoreError 维表写操作统一错误映射：
// NotFound → 404（含跨公司）；AlreadyExists → 409 名称冲突
func failDimensionStoreError(c *gin.Context, err error, what, action string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		Fail(c, http.StatusNotFound, 40401, what+"不存在")
	case errors.Is(err, store.ErrAlreadyExists):
		Fail(c, http.StatusConflict, 40901, "该公司已存在同名"+what)
	default:
		Fail(c, http.StatusInternalServerError, 50001, action+what+"失败")
	}
}

// failDimensionInUse 删除引用拦截：正被引用的维度不允许删（悬空引用会
// 让台账富化丢名字），409 带引用明细
func failDimensionInUse(c *gin.Context, inUse int64, what string) {
	Fail(c, http.StatusConflict, 40902,
		"该"+what+"正被 "+strconv.FormatInt(inUse, 10)+" 条记录引用，请先解除挂接")
}

// countRefs 统计引用数（assets 表；SQLiteStore 测试库无该表，但本方法
// 只在生产 GormStore 的 gin 面执行）
func countRefs(c *gin.Context, modelPtr any, cond string, args ...any) (int64, bool) {
	var inUse int64
	if err := store.DB.Model(modelPtr).Where(cond, args...).Count(&inUse).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询引用失败")
		return 0, false
	}
	return inUse, true
}

// ==================== 厂商 ====================

func (h *DimensionHandler) ListManufacturers(c *gin.Context) {
	var q dimensionListQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		Fail(c, http.StatusBadRequest, 40001, "company_id 必填")
		return
	}
	items, total, err := h.store.ListManufacturers(c.Request.Context(), store.DimensionListFilter{
		CompanyID: q.CompanyID, Keyword: q.Keyword, Page: q.Page, PageSize: q.PageSize,
	})
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询厂商失败")
		return
	}
	Success(c, PageResult{Total: total, Items: items})
}

type manufacturerRequest struct {
	CompanyID int64  `json:"company_id" binding:"required"`
	Name      string `json:"name"`
	Remark    string `json:"remark"`
}

func (h *DimensionHandler) CreateManufacturer(c *gin.Context) {
	var req manufacturerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	created, err := h.store.CreateManufacturer(c.Request.Context(), model.Manufacturer{
		CompanyID: req.CompanyID, Name: req.Name, Remark: req.Remark,
	})
	if err != nil {
		failDimensionStoreError(c, err, "厂商", "创建")
		return
	}
	Success(c, created)
}

func (h *DimensionHandler) UpdateManufacturer(c *gin.Context) {
	id, ok := dimensionIDParam(c)
	if !ok {
		return
	}
	var req manufacturerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	updated, err := h.store.UpdateManufacturer(c.Request.Context(), model.Manufacturer{
		BaseModel: model.BaseModel{ID: id},
		CompanyID: req.CompanyID, Name: req.Name, Remark: req.Remark,
	})
	if err != nil {
		failDimensionStoreError(c, err, "厂商", "更新")
		return
	}
	Success(c, updated)
}

func (h *DimensionHandler) DeleteManufacturer(c *gin.Context) {
	id, ok := dimensionIDParam(c)
	if !ok {
		return
	}
	companyID, ok := dimensionCompanyQuery(c)
	if !ok {
		return
	}
	// 引用拦截：资产挂接 + 型号库挂接
	assetRefs, ok := countRefs(c, &model.Asset{}, "company_id = ? AND manufacturer_id = ?", companyID, id)
	if !ok {
		return
	}
	modelRefs, ok := countRefs(c, &model.AssetModel{}, "company_id = ? AND manufacturer_id = ?", companyID, id)
	if !ok {
		return
	}
	if assetRefs+modelRefs > 0 {
		failDimensionInUse(c, assetRefs+modelRefs, "厂商")
		return
	}
	if err := h.store.DeleteManufacturer(c.Request.Context(), companyID, id); err != nil {
		failDimensionStoreError(c, err, "厂商", "删除")
		return
	}
	Success(c, gin.H{"deleted": true})
}

// ==================== 供应商 ====================

func (h *DimensionHandler) ListSuppliers(c *gin.Context) {
	var q dimensionListQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		Fail(c, http.StatusBadRequest, 40001, "company_id 必填")
		return
	}
	items, total, err := h.store.ListSuppliers(c.Request.Context(), store.DimensionListFilter{
		CompanyID: q.CompanyID, Keyword: q.Keyword, Page: q.Page, PageSize: q.PageSize,
	})
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询供应商失败")
		return
	}
	Success(c, PageResult{Total: total, Items: items})
}

type supplierRequest struct {
	CompanyID   int64  `json:"company_id" binding:"required"`
	Name        string `json:"name"`
	ContactName string `json:"contact_name"`
	Phone       string `json:"phone"`
	Remark      string `json:"remark"`
}

func (h *DimensionHandler) CreateSupplier(c *gin.Context) {
	var req supplierRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	created, err := h.store.CreateSupplier(c.Request.Context(), model.Supplier{
		CompanyID: req.CompanyID, Name: req.Name,
		ContactName: req.ContactName, Phone: req.Phone, Remark: req.Remark,
	})
	if err != nil {
		failDimensionStoreError(c, err, "供应商", "创建")
		return
	}
	Success(c, created)
}

func (h *DimensionHandler) UpdateSupplier(c *gin.Context) {
	id, ok := dimensionIDParam(c)
	if !ok {
		return
	}
	var req supplierRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	updated, err := h.store.UpdateSupplier(c.Request.Context(), model.Supplier{
		BaseModel:   model.BaseModel{ID: id},
		CompanyID:   req.CompanyID, Name: req.Name,
		ContactName: req.ContactName, Phone: req.Phone, Remark: req.Remark,
	})
	if err != nil {
		failDimensionStoreError(c, err, "供应商", "更新")
		return
	}
	Success(c, updated)
}

func (h *DimensionHandler) DeleteSupplier(c *gin.Context) {
	id, ok := dimensionIDParam(c)
	if !ok {
		return
	}
	companyID, ok := dimensionCompanyQuery(c)
	if !ok {
		return
	}
	assetRefs, ok := countRefs(c, &model.Asset{}, "company_id = ? AND supplier_id = ?", companyID, id)
	if !ok {
		return
	}
	if assetRefs > 0 {
		failDimensionInUse(c, assetRefs, "供应商")
		return
	}
	if err := h.store.DeleteSupplier(c.Request.Context(), companyID, id); err != nil {
		failDimensionStoreError(c, err, "供应商", "删除")
		return
	}
	Success(c, gin.H{"deleted": true})
}

// ==================== 位置库 ====================

func (h *DimensionHandler) ListLocations(c *gin.Context) {
	var q dimensionListQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		Fail(c, http.StatusBadRequest, 40001, "company_id 必填")
		return
	}
	items, total, err := h.store.ListLocations(c.Request.Context(), store.DimensionListFilter{
		CompanyID: q.CompanyID, Keyword: q.Keyword, Page: q.Page, PageSize: q.PageSize,
	})
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询位置失败")
		return
	}
	enrichLocationParents(c.Request.Context(), items)
	Success(c, PageResult{Total: total, Items: items})
}

// enrichLocationParents 批量补 parent_name（一次 IN 查询，防 N+1）
func enrichLocationParents(ctx context.Context, items []model.Location) {
	parentIDs := make([]int64, 0, len(items))
	for _, l := range items {
		if l.ParentID != nil {
			parentIDs = append(parentIDs, *l.ParentID)
		}
	}
	if len(parentIDs) == 0 {
		return
	}
	var parents []model.Location
	if err := store.DB.WithContext(ctx).Where("id IN ?", parentIDs).Find(&parents).Error; err != nil {
		return // 富化失败不阻塞列表，前端按无父级名展示
	}
	names := make(map[int64]string, len(parents))
	for _, p := range parents {
		names[p.ID] = p.Name
	}
	for i := range items {
		if items[i].ParentID != nil {
			items[i].ParentName = names[*items[i].ParentID]
		}
	}
}

type locationRequest struct {
	CompanyID int64  `json:"company_id" binding:"required"`
	Name      string `json:"name"`
	ParentID  *int64 `json:"parent_id"` // NULL/0 = 顶级
	Remark    string `json:"remark"`
}

// validateLocationParent 父位置校验：存在 + 同公司（否则 404）、
// 非自身且不构成环（否则 400）。环检测沿父链上溯找自身，上溯超过
// maxLocationDepth 视为脏环数据拒绝，防无限循环
const maxLocationDepth = 100

func (h *DimensionHandler) validateLocationParent(c *gin.Context, companyID, selfID int64, parentID *int64) bool {
	if parentID == nil || *parentID == 0 {
		return true
	}
	if *parentID == selfID {
		Fail(c, http.StatusBadRequest, 40003, "位置不能作为自身的父级")
		return false
	}
	var parent model.Location
	if err := store.DB.Where("id = ? AND company_id = ?", *parentID, companyID).First(&parent).Error; err != nil {
		Fail(c, http.StatusNotFound, 40402, "父位置不存在或不属于该公司")
		return false
	}
	// 沿父链上溯：途中遇到自身说明新父链绕回，构成环
	cur := parent
	for i := 0; i < maxLocationDepth && cur.ParentID != nil; i++ {
		if *cur.ParentID == selfID {
			Fail(c, http.StatusBadRequest, 40003, "不能把位置挂到自己的子孙节点下（构成环）")
			return false
		}
		var up model.Location
		if err := store.DB.Where("id = ? AND company_id = ?", *cur.ParentID, companyID).First(&up).Error; err != nil {
			break // 父链断裂按脏数据放行，避免卡住治理动作
		}
		cur = up
	}
	return true
}

func (h *DimensionHandler) CreateLocation(c *gin.Context) {
	var req locationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	if !h.validateLocationParent(c, req.CompanyID, 0, req.ParentID) {
		return
	}
	normalizeZeroRef(&req.ParentID) // parent_id=0 归一为顶级
	created, err := h.store.CreateLocation(c.Request.Context(), model.Location{
		CompanyID: req.CompanyID, Name: req.Name, ParentID: req.ParentID, Remark: req.Remark,
	})
	if err != nil {
		failDimensionStoreError(c, err, "位置", "创建")
		return
	}
	Success(c, created)
}

func (h *DimensionHandler) UpdateLocation(c *gin.Context) {
	id, ok := dimensionIDParam(c)
	if !ok {
		return
	}
	var req locationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	if !h.validateLocationParent(c, req.CompanyID, id, req.ParentID) {
		return
	}
	normalizeZeroRef(&req.ParentID) // parent_id=0 归一为顶级
	updated, err := h.store.UpdateLocation(c.Request.Context(), model.Location{
		BaseModel: model.BaseModel{ID: id},
		CompanyID: req.CompanyID, Name: req.Name, ParentID: req.ParentID, Remark: req.Remark,
	})
	if err != nil {
		failDimensionStoreError(c, err, "位置", "更新")
		return
	}
	Success(c, updated)
}

func (h *DimensionHandler) DeleteLocation(c *gin.Context) {
	id, ok := dimensionIDParam(c)
	if !ok {
		return
	}
	companyID, ok := dimensionCompanyQuery(c)
	if !ok {
		return
	}
	// 引用拦截：资产挂接 + 子位置
	assetRefs, ok := countRefs(c, &model.Asset{}, "company_id = ? AND location_id = ?", companyID, id)
	if !ok {
		return
	}
	childRefs, ok := countRefs(c, &model.Location{}, "company_id = ? AND parent_id = ?", companyID, id)
	if !ok {
		return
	}
	if assetRefs+childRefs > 0 {
		failDimensionInUse(c, assetRefs+childRefs, "位置")
		return
	}
	if err := h.store.DeleteLocation(c.Request.Context(), companyID, id); err != nil {
		failDimensionStoreError(c, err, "位置", "删除")
		return
	}
	Success(c, gin.H{"deleted": true})
}

// ==================== 型号库 ====================

func (h *DimensionHandler) ListAssetModels(c *gin.Context) {
	var q assetModelListQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		Fail(c, http.StatusBadRequest, 40001, "company_id 必填")
		return
	}
	items, total, err := h.store.ListAssetModels(c.Request.Context(), store.AssetModelListFilter{
		CompanyID: q.CompanyID, CategoryID: q.CategoryID, Keyword: q.Keyword,
		Page: q.Page, PageSize: q.PageSize,
	})
	if err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询型号失败")
		return
	}
	enrichAssetModelNames(c.Request.Context(), items)
	Success(c, PageResult{Total: total, Items: items})
}

// enrichAssetModelNames 批量补 manufacturer_name / depreciation_name
// （悬空引用安静降级为空，不阻塞列表）
func enrichAssetModelNames(ctx context.Context, items []model.AssetModel) {
	manufacturerIDs := make([]int64, 0, len(items))
	depreciationIDs := make([]int64, 0, len(items))
	for _, m := range items {
		if m.ManufacturerID != nil {
			manufacturerIDs = append(manufacturerIDs, *m.ManufacturerID)
		}
		if m.DepreciationID != nil {
			depreciationIDs = append(depreciationIDs, *m.DepreciationID)
		}
	}
	manufacturerNames := make(map[int64]string)
	if len(manufacturerIDs) > 0 {
		var manufacturers []model.Manufacturer
		if err := store.DB.WithContext(ctx).Where("id IN ?", manufacturerIDs).Find(&manufacturers).Error; err == nil {
			for _, m := range manufacturers {
				manufacturerNames[m.ID] = m.Name
			}
		}
	}
	depreciationNames := make(map[int64]string)
	if len(depreciationIDs) > 0 {
		var rules []model.DepreciationRule
		if err := store.DB.WithContext(ctx).Where("id IN ?", depreciationIDs).Find(&rules).Error; err == nil {
			for _, r := range rules {
				depreciationNames[r.ID] = r.Name
			}
		}
	}
	for i := range items {
		if items[i].ManufacturerID != nil {
			items[i].ManufacturerName = manufacturerNames[*items[i].ManufacturerID]
		}
		if items[i].DepreciationID != nil {
			items[i].DepreciationName = depreciationNames[*items[i].DepreciationID]
		}
	}
}

type assetModelRequest struct {
	CompanyID      int64  `json:"company_id" binding:"required"`
	Name           string `json:"name"`
	CategoryID     int64  `json:"category_id"`     // 0 = 不限类别
	ManufacturerID *int64 `json:"manufacturer_id"` // NULL/0 = 不挂
	DepreciationID *int64 `json:"depreciation_id"` // NULL/0 = 不挂
	EOLMonths      int    `json:"eol_months"`      // 0 = 不限
	Remark         string `json:"remark"`
}

// validateAssetModelRefs 型号库外键校验：厂商/折旧规则存在且同公司；
// 0/负值统一归一为 nil（不挂接），防止把 0 写进外键列
func (h *DimensionHandler) validateAssetModelRefs(c *gin.Context, req *assetModelRequest) bool {
	if req.ManufacturerID != nil && *req.ManufacturerID > 0 {
		var m model.Manufacturer
		if err := store.DB.Where("id = ? AND company_id = ?", *req.ManufacturerID, req.CompanyID).First(&m).Error; err != nil {
			Fail(c, http.StatusNotFound, 40402, "厂商不存在或不属于该公司")
			return false
		}
	} else {
		req.ManufacturerID = nil
	}
	if req.DepreciationID != nil && *req.DepreciationID > 0 {
		var r model.DepreciationRule
		if err := store.DB.Where("id = ? AND company_id = ?", *req.DepreciationID, req.CompanyID).First(&r).Error; err != nil {
			Fail(c, http.StatusNotFound, 40402, "折旧规则不存在或不属于该公司")
			return false
		}
	} else {
		req.DepreciationID = nil
	}
	return true
}

func (h *DimensionHandler) CreateAssetModel(c *gin.Context) {
	var req assetModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	if !h.validateAssetModelRefs(c, &req) {
		return
	}
	created, err := h.store.CreateAssetModel(c.Request.Context(), model.AssetModel{
		CompanyID: req.CompanyID, Name: req.Name, CategoryID: req.CategoryID,
		ManufacturerID: req.ManufacturerID, DepreciationID: req.DepreciationID,
		EOLMonths: req.EOLMonths, Remark: req.Remark,
	})
	if err != nil {
		failDimensionStoreError(c, err, "型号", "创建")
		return
	}
	Success(c, created)
}

func (h *DimensionHandler) UpdateAssetModel(c *gin.Context) {
	id, ok := dimensionIDParam(c)
	if !ok {
		return
	}
	var req assetModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	if !h.validateAssetModelRefs(c, &req) {
		return
	}
	updated, err := h.store.UpdateAssetModel(c.Request.Context(), model.AssetModel{
		BaseModel: model.BaseModel{ID: id},
		CompanyID: req.CompanyID, Name: req.Name, CategoryID: req.CategoryID,
		ManufacturerID: req.ManufacturerID, DepreciationID: req.DepreciationID,
		EOLMonths: req.EOLMonths, Remark: req.Remark,
	})
	if err != nil {
		failDimensionStoreError(c, err, "型号", "更新")
		return
	}
	Success(c, updated)
}

func (h *DimensionHandler) DeleteAssetModel(c *gin.Context) {
	id, ok := dimensionIDParam(c)
	if !ok {
		return
	}
	companyID, ok := dimensionCompanyQuery(c)
	if !ok {
		return
	}
	assetRefs, ok := countRefs(c, &model.Asset{}, "company_id = ? AND model_id = ?", companyID, id)
	if !ok {
		return
	}
	if assetRefs > 0 {
		failDimensionInUse(c, assetRefs, "型号")
		return
	}
	if err := h.store.DeleteAssetModel(c.Request.Context(), companyID, id); err != nil {
		failDimensionStoreError(c, err, "型号", "删除")
		return
	}
	Success(c, gin.H{"deleted": true})
}

// ==================== 资产侧维度联动（P1）====================

// normalizeZeroRef 外键参数 0/负值归一为 nil（前端清空下拉传 0 的约定），
// 防止把 0 写进外键列
func normalizeZeroRef(ref **int64) {
	if *ref != nil && **ref <= 0 {
		*ref = nil
	}
}

// dimensionRefOK 维度外键解析：不存在/跨公司统一 404（公司边界语义）
func dimensionRefOK(c *gin.Context, dest any, id, companyID int64, what string) bool {
	if err := store.DB.Where("id = ? AND company_id = ?", id, companyID).First(dest).Error; err != nil {
		Fail(c, http.StatusNotFound, 40402, what+"不存在或不属于该公司")
		return false
	}
	return true
}

// assetDimensionLinks 建账维度解析结果（只含通过校验的挂接）
type assetDimensionLinks struct {
	manufacturer *model.Manufacturer
	assetModel   *model.AssetModel
	supplier     *model.Supplier
	location     *model.Location
}

// resolveAssetDimensionLinks 建账维度外键校验：存在 + 同公司；型号类别
// 必须与资产类别匹配（0 = 不限）。型号带厂商而建账未显式选厂商时随
// 型号带出（保证品牌快照与外键一致）。任一失败即以对应错误码终止
func resolveAssetDimensionLinks(c *gin.Context, companyID, categoryID int64,
	manufacturerID, modelID, supplierID, locationID *int64) (*assetDimensionLinks, bool) {
	links := &assetDimensionLinks{}

	if manufacturerID != nil && *manufacturerID > 0 {
		var m model.Manufacturer
		if !dimensionRefOK(c, &m, *manufacturerID, companyID, "厂商") {
			return nil, false
		}
		links.manufacturer = &m
	}
	if modelID != nil && *modelID > 0 {
		var am model.AssetModel
		if !dimensionRefOK(c, &am, *modelID, companyID, "型号") {
			return nil, false
		}
		if am.CategoryID > 0 && am.CategoryID != categoryID {
			Fail(c, http.StatusBadRequest, 40003, "型号适用类别与资产类别不匹配")
			return nil, false
		}
		links.assetModel = &am
		if links.manufacturer == nil && am.ManufacturerID != nil {
			var m model.Manufacturer
			if err := store.DB.Where("id = ? AND company_id = ?", *am.ManufacturerID, companyID).
				First(&m).Error; err == nil {
				links.manufacturer = &m
			}
		}
	}
	if supplierID != nil && *supplierID > 0 {
		var sup model.Supplier
		if !dimensionRefOK(c, &sup, *supplierID, companyID, "供应商") {
			return nil, false
		}
		links.supplier = &sup
	}
	if locationID != nil && *locationID > 0 {
		var loc model.Location
		if !dimensionRefOK(c, &loc, *locationID, companyID, "位置") {
			return nil, false
		}
		links.location = &loc
	}
	return links, true
}

// applyAssetDimensionLinks 把解析好的维度落到台账（外键 + 快照写回 +
// 折旧规则继承：建账未显式挂规则时继承型号库预挂规则，编辑不自动改）
func applyAssetDimensionLinks(asset *model.Asset, links *assetDimensionLinks, explicitDepreciationID *int64) {
	if links.manufacturer != nil {
		id := links.manufacturer.ID
		asset.ManufacturerID = &id
		asset.Brand = links.manufacturer.Name
	}
	if links.assetModel != nil {
		id := links.assetModel.ID
		asset.ModelID = &id
		asset.ModelName = links.assetModel.Name
	}
	if links.supplier != nil {
		id := links.supplier.ID
		asset.SupplierID = &id
	}
	if links.location != nil {
		id := links.location.ID
		asset.LocationID = &id
		asset.Location = links.location.Name
	}
	if explicitDepreciationID == nil && links.assetModel != nil && links.assetModel.DepreciationID != nil {
		asset.DepreciationID = links.assetModel.DepreciationID
	}
}

// applyAssetDimensionUpdates 台账编辑的维度外键处理：>0 校验并挂接
// （同步写回 brand / model_name / location 快照）；0 解除挂接（快照文本
// 保留，历史兼容）。型号类别按"本次生效类别"校验（类别可同请求变更）
func applyAssetDimensionUpdates(c *gin.Context, asset *model.Asset, req *UpdateAssetRequest, updates map[string]any) bool {
	effectiveCategory := asset.CategoryID
	if req.CategoryID != nil {
		effectiveCategory = *req.CategoryID
	}

	if req.ManufacturerID != nil {
		if *req.ManufacturerID > 0 {
			var m model.Manufacturer
			if !dimensionRefOK(c, &m, *req.ManufacturerID, asset.CompanyID, "厂商") {
				return false
			}
			updates["manufacturer_id"] = m.ID
			updates["brand"] = m.Name
		} else {
			updates["manufacturer_id"] = nil
		}
	}
	if req.ModelID != nil {
		if *req.ModelID > 0 {
			var am model.AssetModel
			if !dimensionRefOK(c, &am, *req.ModelID, asset.CompanyID, "型号") {
				return false
			}
			if am.CategoryID > 0 && am.CategoryID != effectiveCategory {
				Fail(c, http.StatusBadRequest, 40003, "型号适用类别与资产类别不匹配")
				return false
			}
			updates["model_id"] = am.ID
			updates["model_name"] = am.Name
		} else {
			updates["model_id"] = nil
		}
	}
	if req.SupplierID != nil {
		if *req.SupplierID > 0 {
			var sup model.Supplier
			if !dimensionRefOK(c, &sup, *req.SupplierID, asset.CompanyID, "供应商") {
				return false
			}
			updates["supplier_id"] = sup.ID
		} else {
			updates["supplier_id"] = nil
		}
	}
	if req.LocationID != nil {
		if *req.LocationID > 0 {
			var loc model.Location
			if !dimensionRefOK(c, &loc, *req.LocationID, asset.CompanyID, "位置") {
				return false
			}
			updates["location_id"] = loc.ID
			updates["location"] = loc.Name
		} else {
			updates["location_id"] = nil
		}
	}
	return true
}

// enrichAssetsDimensions 资产维度富化：外键存在且能解析到维表时，以
// 维表名覆盖台账展示字段（维度重命名可传播），供应商回填 SupplierName；
// 解析不到（理论不存在：删除已做引用拦截）保留原快照不覆盖。
// 批量 IN 查询防 N+1；资产列表 / 详情 / Excel 导出三处共用，口径只此一处
func enrichAssetsDimensions(ctx context.Context, items []model.Asset) error {
	if len(items) == 0 {
		return nil
	}
	collect := func(pick func(model.Asset) *int64) []int64 {
		ids := make([]int64, 0, len(items))
		for i := range items {
			if id := pick(items[i]); id != nil {
				ids = append(ids, *id)
			}
		}
		return ids
	}

	manufacturerNames := map[int64]string{}
	if ids := collect(func(a model.Asset) *int64 { return a.ManufacturerID }); len(ids) > 0 {
		var rows []model.Manufacturer
		if err := store.DB.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return fmt.Errorf("query manufacturers: %w", err)
		}
		for _, m := range rows {
			manufacturerNames[m.ID] = m.Name
		}
	}
	modelNames := map[int64]string{}
	if ids := collect(func(a model.Asset) *int64 { return a.ModelID }); len(ids) > 0 {
		var rows []model.AssetModel
		if err := store.DB.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return fmt.Errorf("query asset models: %w", err)
		}
		for _, m := range rows {
			modelNames[m.ID] = m.Name
		}
	}
	locationNames := map[int64]string{}
	if ids := collect(func(a model.Asset) *int64 { return a.LocationID }); len(ids) > 0 {
		var rows []model.Location
		if err := store.DB.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return fmt.Errorf("query locations: %w", err)
		}
		for _, l := range rows {
			locationNames[l.ID] = l.Name
		}
	}
	supplierNames := map[int64]string{}
	if ids := collect(func(a model.Asset) *int64 { return a.SupplierID }); len(ids) > 0 {
		var rows []model.Supplier
		if err := store.DB.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return fmt.Errorf("query suppliers: %w", err)
		}
		for _, s := range rows {
			supplierNames[s.ID] = s.Name
		}
	}
	licenseNames := map[int64]string{}
	if ids := collect(func(a model.Asset) *int64 { return a.LicenseID }); len(ids) > 0 {
		var rows []model.License
		if err := store.DB.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return fmt.Errorf("query licenses: %w", err)
		}
		for _, l := range rows {
			licenseNames[l.ID] = l.Name
		}
	}

	for i := range items {
		if items[i].ManufacturerID != nil {
			if name, ok := manufacturerNames[*items[i].ManufacturerID]; ok {
				items[i].Brand = name
			}
		}
		if items[i].ModelID != nil {
			if name, ok := modelNames[*items[i].ModelID]; ok {
				items[i].ModelName = name
			}
		}
		if items[i].LocationID != nil {
			if name, ok := locationNames[*items[i].LocationID]; ok {
				items[i].Location = name
			}
		}
		if items[i].SupplierID != nil {
			items[i].SupplierName = supplierNames[*items[i].SupplierID]
		}
		if items[i].LicenseID != nil {
			items[i].LicenseName = licenseNames[*items[i].LicenseID]
		}
	}
	return nil
}
