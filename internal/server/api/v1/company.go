package v1

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"itagent/internal/server/api/middleware"
	"itagent/internal/server/model"
	"itagent/internal/server/store"

	"github.com/gin-gonic/gin"
)

// 公司（多租户）CRUD：读面登录可读（各页公司下拉依赖），写面 admin。
// 名称全局唯一（含软删行——DB 唯一索引不滤软删，查重同口径 Unscoped，
// 避免「查重说可用、插入撞索引」的 500）；删除有全业务表引用拦截

type CompanyHandler struct{}

func RegisterCompanyRoutes(protected *gin.RouterGroup) {
	h := &CompanyHandler{}
	read := protected.Group("/companies")
	{
		read.GET("", h.List)
		read.GET("/:id", h.Get)
	}
	adminOnly := protected.Group("/companies")
	adminOnly.Use(middleware.RoleMiddleware("admin"))
	{
		adminOnly.POST("", h.Create)
		adminOnly.PUT("/:id", h.Update)
		adminOnly.DELETE("/:id", h.Delete)
	}
}

func (h *CompanyHandler) List(c *gin.Context) {
	var list []model.Company
	if err := store.DB.Find(&list).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "查询公司失败")
		return
	}
	Success(c, list)
}

func (h *CompanyHandler) Get(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid company id")
		return
	}
	var company model.Company
	if err := store.DB.First(&company, id).Error; err != nil {
		Fail(c, http.StatusNotFound, 40401, "company not found")
		return
	}
	Success(c, company)
}

type CreateCompanyRequest struct {
	Name   string `json:"name" binding:"required"`
	Code   string `json:"code" binding:"required"`
	Domain string `json:"domain"`
}

// companyNameTaken 名称占用检查：与 DB 唯一索引同口径（Unscoped 含软删行）
func companyNameTaken(id int64, name string) bool {
	q := store.DB.Unscoped().Model(&model.Company{}).Where("name = ?", name)
	if id > 0 {
		q = q.Where("id <> ?", id)
	}
	var cnt int64
	if err := q.Count(&cnt).Error; err != nil {
		return true // 校验失败按占用处理，拒绝写入
	}
	return cnt > 0
}

func (h *CompanyHandler) Create(c *gin.Context) {
	var req CreateCompanyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		Fail(c, http.StatusBadRequest, 40001, "公司名称必填")
		return
	}
	if companyNameTaken(0, name) {
		Fail(c, http.StatusConflict, 40901, "已存在同名公司")
		return
	}
	company := model.Company{
		Name:   name,
		Code:   strings.TrimSpace(req.Code),
		Domain: strings.TrimSpace(req.Domain),
	}
	if err := store.DB.Create(&company).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50002, "创建公司失败")
		return
	}
	Success(c, company)
}

type UpdateCompanyRequest struct {
	Name   string `json:"name" binding:"required"`
	Code   string `json:"code"`
	Domain string `json:"domain"`
}

func (h *CompanyHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid company id")
		return
	}
	var req UpdateCompanyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, 40001, err.Error())
		return
	}
	var company model.Company
	if err := store.DB.First(&company, id).Error; err != nil {
		Fail(c, http.StatusNotFound, 40401, "公司不存在")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		Fail(c, http.StatusBadRequest, 40001, "公司名称必填")
		return
	}
	if companyNameTaken(company.ID, name) {
		Fail(c, http.StatusConflict, 40901, "已存在同名公司")
		return
	}
	updates := map[string]interface{}{
		"name":   name,
		"code":   strings.TrimSpace(req.Code),
		"domain": strings.TrimSpace(req.Domain),
		// 兼容既有写法：更新时也把 updated_at 归一（UTC）
		"updated_at": time.Now().UTC(),
	}
	if err := store.DB.Model(&company).Updates(updates).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50003, "更新公司失败")
		return
	}
	if err := store.DB.First(&company, id).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50001, "重载公司失败")
		return
	}
	Success(c, company)
}

// companyRefTables 公司删除引用拦截的表清单（事实唯一源）：新增带
// company_id 的业务表时必须同步登记，否则删除会留孤儿行
var companyRefTables = []struct {
	modelPtr any
	what     string
}{
	{&model.Asset{}, "资产"},
	{&model.User{}, "用户"},
	{&model.Department{}, "部门"},
	{&model.AssetDispatch{}, "外派登记"},
	{&model.AssetRequest{}, "设备申请"},
	{&model.License{}, "软件许可"},
	{&model.Consumable{}, "耗材"},
	{&model.ConsumableTxn{}, "耗材流水"},
	{&model.SoftwarePool{}, "受控软件"},
	{&model.DeviceSoftware{}, "终端软件"},
	{&model.StorageLending{}, "移动存储领用"},
	{&model.PartRecord{}, "配件流水"},
	{&model.Manufacturer{}, "厂商"},
	{&model.Supplier{}, "供应商"},
	{&model.Location{}, "位置"},
	{&model.AssetModel{}, "型号"},
	{&model.Stocktake{}, "盘点任务"},
	{&model.Notification{}, "站内信"},
}

// Delete 删除公司：任一业务表仍有**活跃引用**即 409 带明细。
// 只算活跃行（默认软删 scope）——软删行视为已死（展示层不可见），
// 若计入会出现「删资产（软删）→ 删公司」被软删行死锁、且无任何 UI
// 途径清理的死局；孤儿软删行随公司删除残留是可接受代价
func (h *CompanyHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, 40002, "invalid company id")
		return
	}
	var company model.Company
	if err := store.DB.First(&company, id).Error; err != nil {
		Fail(c, http.StatusNotFound, 40401, "公司不存在")
		return
	}
	var blocked []string
	for _, t := range companyRefTables {
		var cnt int64
		if err := store.DB.Model(t.modelPtr).
			Where("company_id = ?", company.ID).Count(&cnt).Error; err != nil {
			Fail(c, http.StatusInternalServerError, 50001, "查询公司引用失败")
			return
		}
		if cnt > 0 {
			blocked = append(blocked, t.what+" "+strconv.FormatInt(cnt, 10))
		}
	}
	if len(blocked) > 0 {
		Fail(c, http.StatusConflict, 40902,
			"该公司仍有业务数据引用（"+strings.Join(blocked, "、")+"），请先迁移或清理")
		return
	}
	// 零引用才可删 → 硬删（软删会让唯一索引永久占名，同名公司无法重建）
	if err := store.DB.Unscoped().Delete(&company).Error; err != nil {
		Fail(c, http.StatusInternalServerError, 50003, "删除公司失败")
		return
	}
	Success(c, gin.H{"deleted": true})
}
